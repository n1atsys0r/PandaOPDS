package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pandaopds/internal/eh"
	"pandaopds/internal/home"
	"pandaopds/internal/storage"
	"pandaopds/internal/subscription"
)

func TestSubscriptionOPDSReadsOnlyLocalCacheAndPaginates(t *testing.T) {
	requests := 0
	server := newHomePreviewServer(t, "", homePreviewTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		return homePreviewResponse(r, `<html></html>`), nil
	}))
	db, err := storage.Open(filepath.Join(t.TempDir(), "subscriptions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := subscription.New(db.DB)
	created, err := store.Create(context.Background(), subscription.Config{Name: "Popular cache", Source: "preset", Query: map[string]any{"preset": "popular"}})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := store.Create(context.Background(), subscription.Config{Name: "Empty cache", Source: "preset", Query: map[string]any{"preset": "toplist:month"}})
	if err != nil {
		t.Fatal(err)
	}
	server.Subscriptions = subscription.NewWorker(db.DB, server.Service, nil, nil, server.Settings)

	pageCount := 12
	cached := make([]eh.GalleryListItem, 55)
	for i := range cached {
		gid := 5000 + i
		cached[i] = eh.GalleryListItem{GID: gid, Token: "abcdef12", Title: fmt.Sprintf("Cached %02d", i+1), Category: "Manga", CoverURL: fmt.Sprintf("/api/publications/eh/%d/abcdef12/cover", gid), PageCount: &pageCount, Rating: 4.5}
	}
	if _, err = db.Exec(`INSERT INTO subscription_runs(id,subscription_id,started_at,status,revision) VALUES('cached-run',?,?, 'running',1)`, created.ID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.AddPage(context.Background(), created.ID, "cached-run", 1, 1, "", false, false, cached); err != nil {
		t.Fatal(err)
	}

	readFeed := func(id string, page int) (int, map[string]any) {
		r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/opds/v2.0/subscription/%s?page=%d", id, page), nil)
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		server.subscriptionFeed20(w, r)
		var doc map[string]any
		if w.Code == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
				t.Fatal(err)
			}
		}
		return w.Code, doc
	}
	status, first := readFeed(created.ID, 1)
	if status != http.StatusOK {
		t.Fatalf("page 1 status=%d", status)
	}
	pubs := first["publications"].([]any)
	if len(pubs) != 50 {
		t.Fatalf("page 1 has %d publications", len(pubs))
	}
	links := first["links"].([]any)
	foundNext := false
	for _, raw := range links {
		link := raw.(map[string]any)
		if link["rel"] == "next" {
			foundNext = strings.HasPrefix(link["href"].(string), "https://reader.example/opds/v2.0/subscription/") && strings.HasSuffix(link["href"].(string), "?page=2")
		}
	}
	if !foundNext {
		t.Fatalf("page 1 next link missing/incorrect: %#v", links)
	}
	firstPub := pubs[0].(map[string]any)
	images := firstPub["images"].([]any)
	if got := images[0].(map[string]any)["href"].(string); !strings.HasPrefix(got, "https://reader.example/api/publications/eh/") {
		t.Fatalf("image did not use cached local cover URL: %s", got)
	}
	metadata := firstPub["metadata"].(map[string]any)
	if metadata["numberOfPages"] != float64(12) {
		t.Fatalf("cached page count not carried into publication: %#v", metadata)
	}
	status, second := readFeed(created.ID, 2)
	if status != http.StatusOK || len(second["publications"].([]any)) != 5 {
		t.Fatalf("page 2 status=%d body=%#v", status, second)
	}
	if status, _ = readFeed("missing", 1); status != http.StatusNotFound {
		t.Fatalf("missing subscription status=%d", status)
	}

	// Repeated sources share one cache query sized to the largest requested preview.
	count12, count20 := 12, 20
	cfg := home.Config{Pages: []home.Page{{ID: "home", Title: "Home", Sections: []home.Section{
		{Kind: "publication", Type: "subscription", Query: created.ID, Count: &count12},
		{Kind: "publication", Group: true, Type: "subscription", Query: created.ID, Count: &count20},
		{Kind: "facets", Type: "search", Query: "should-not-link-upstream", Items: []home.NavigationItem{{Title: "Manga", Categories: []string{"Manga"}}}},
	}}}}
	w := httptest.NewRecorder()
	server.renderPageV20(w, httptest.NewRequest(http.MethodGet, "/opds/v2.0", nil), cfg, cfg.Pages[0], true)
	var homeDoc map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &homeDoc); err != nil {
		t.Fatal(err)
	}
	if len(homeDoc["publications"].([]any)) != 12 {
		t.Fatalf("ungrouped preview did not honor count 12")
	}
	groups := homeDoc["groups"].([]any)
	if len(groups) != 1 || len(groups[0].(map[string]any)["publications"].([]any)) != 20 {
		t.Fatalf("shared subscription preview did not honor max count 20: %#v", groups)
	}
	if _, ok := homeDoc["facets"]; ok {
		t.Fatalf("subscription feed exposed facets pointing to live search: %#v", homeDoc["facets"])
	}

	for _, id := range []string{empty.ID, "missing"} {
		cfg.Pages[0].Sections = []home.Section{{Kind: "publication", Type: "subscription", Query: id}, {Kind: "facets", Items: []home.NavigationItem{{Title: "Manga", Categories: []string{"Manga"}}}}}
		w = httptest.NewRecorder()
		server.renderPageV20(w, httptest.NewRequest(http.MethodGet, "/opds/v2.0", nil), cfg, cfg.Pages[0], true)
		body := w.Body.String()
		if strings.Contains(body, `"title": "Latest"`) || strings.Contains(body, `"facets"`) {
			t.Fatalf("empty/missing subscription fell back to live feed/facets: %s", body)
		}
	}
	if requests != 0 {
		t.Fatalf("subscription OPDS triggered %d upstream list requests", requests)
	}
}

func TestSubscriptionOPDSNormalizesGroupedTags(t *testing.T) {
	server := newHomePreviewServer(t, "", homePreviewTransport(func(r *http.Request) (*http.Response, error) {
		return homePreviewResponse(r, `<html></html>`), nil
	}))
	db, err := storage.Open(filepath.Join(t.TempDir(), "subscriptions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := subscription.New(db.DB)
	cfg, err := store.Create(context.Background(), subscription.Config{Name: "Grouped tags", Source: "preset", Query: map[string]any{"preset": "popular"}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	metadata := `{"title":"Grouped tags","category":"Manga","tags":{"artist":[{"key":"alice","status":"confidence"}]}}`
	if _, err = db.Exec(`INSERT INTO publications(provider,gid,token,metadata_json,title,category,created_at,updated_at) VALUES('eh',9001,'abcdef12',?,'Grouped tags','Manga',?,?)`, metadata, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO subscription_items(subscription_id,publication_id,first_seen_at,last_seen_at) SELECT ?,id,?,? FROM publications WHERE provider='eh' AND gid=9001 AND token='abcdef12'`, cfg.ID, now, now); err != nil {
		t.Fatal(err)
	}
	server.Subscriptions = subscription.NewWorker(db.DB, server.Service, nil, nil, server.Settings)
	r := httptest.NewRequest(http.MethodGet, "/opds/v2.0/subscription/"+cfg.ID, nil)
	r.SetPathValue("id", cfg.ID)
	w := httptest.NewRecorder()
	server.subscriptionFeed20(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("subscription feed status=%d body=%s", w.Code, w.Body)
	}
	var doc map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	publications := doc["publications"].([]any)
	if len(publications) != 1 {
		t.Fatalf("grouped tags dropped publication: %#v", publications)
	}
	metadataOut := publications[0].(map[string]any)["metadata"].(map[string]any)
	subjects := metadataOut["subject"].([]any)
	if len(subjects) != 2 || subjects[0].(map[string]any)["name"] != "category:Manga" || subjects[1].(map[string]any)["name"] != "artist:alice" {
		t.Fatalf("subscription subjects=%#v", subjects)
	}
}
