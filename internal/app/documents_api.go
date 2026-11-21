package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"pandaopds/internal/config"
	"pandaopds/internal/document"
	"pandaopds/internal/feed"
)

// RegisterDocumentRoutes installs the local-document API and offline reading
// routes on the server mux.
func RegisterDocumentRoutes(mux *http.ServeMux, store *document.Store, settings config.Settings) {
	if mux == nil || store == nil {
		return
	}
	mux.HandleFunc("GET /api/documents", func(w http.ResponseWriter, r *http.Request) { documentList(w, r, store) })
	mux.HandleFunc("GET /api/documents/{id}", func(w http.ResponseWriter, r *http.Request) { documentGet(w, r, store, settings) })
	mux.HandleFunc("POST /api/documents/{id}/associate", func(w http.ResponseWriter, r *http.Request) { documentAssociate(w, r, store) })
	mux.HandleFunc("DELETE /api/documents/{id}", func(w http.ResponseWriter, r *http.Request) { documentDelete(w, r, store) })
	mux.HandleFunc("DELETE /api/documents/{id}/record", func(w http.ResponseWriter, r *http.Request) { documentPurgeMissing(w, r, store) })
	mux.HandleFunc("GET /opds/v2.0/documents/{id}/manifest", func(w http.ResponseWriter, r *http.Request) { documentManifest(w, r, store, settings) })
	mux.HandleFunc("GET /stream/documents/{id}/page/{page}", func(w http.ResponseWriter, r *http.Request) { documentPage(w, r, store, settings) })
	mux.HandleFunc("GET /image/documents/{id}/thumb", func(w http.ResponseWriter, r *http.Request) { documentCover(w, r, store) })
	mux.HandleFunc("/opds/progression/documents/{id}", func(w http.ResponseWriter, r *http.Request) { documentProgression(w, r, store) })
}

func documentList(w http.ResponseWriter, r *http.Request, store *document.Store) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	items, total, err := store.List(r.Context(), page, per, r.URL.Query().Get("status"), r.URL.Query().Get("linked"))
	if err != nil {
		writeJSON(w, 500, map[string]any{"detail": "could not list local documents"})
		return
	}
	result := make([]map[string]any, 0, len(items))
	for i := range items {
		result = append(result, decorateDocument(r, items[i], store))
	}
	if page < 1 {
		page = 1
	}
	if per < 1 {
		per = 50
	}
	if per > 200 {
		per = 200
	}
	writeJSON(w, 200, map[string]any{"items": result, "total": total, "page": page, "per_page": per})
}
func documentGet(w http.ResponseWriter, r *http.Request, store *document.Store, settings config.Settings) {
	d, err := store.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, document.ErrNotFound) {
		writeJSON(w, 404, map[string]any{"detail": "document not found"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]any{"detail": "could not read document"})
		return
	}
	writeJSON(w, 200, decorateDocument(r, d, store))
}
func documentAssociate(w http.ResponseWriter, r *http.Request, store *document.Store) {
	var body struct {
		GID   int    `json:"gid"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]any{"detail": "invalid association"})
		return
	}
	if err := store.Associate(r.Context(), r.PathValue("id"), body.GID, body.Token); err != nil {
		if errors.Is(err, document.ErrNotFound) {
			writeJSON(w, 404, map[string]any{"detail": "document not found"})
		} else {
			writeJSON(w, 400, map[string]any{"detail": err.Error()})
		}
		return
	}
	documentGet(w, r, store, config.Settings{})
}
func documentDelete(w http.ResponseWriter, r *http.Request, store *document.Store) {
	if err := store.Delete(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, document.ErrNotFound) {
			writeJSON(w, 404, map[string]any{"detail": "document not found"})
		} else {
			writeJSON(w, 409, map[string]any{"detail": "could not remove local document"})
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func documentPurgeMissing(w http.ResponseWriter, r *http.Request, store *document.Store) {
	if err := store.PurgeMissing(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, document.ErrNotFound) {
			writeJSON(w, 404, map[string]any{"detail": "only missing document records can be cleared"})
		} else {
			writeJSON(w, 500, map[string]any{"detail": "could not clear missing document record"})
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func decorateDocument(r *http.Request, d document.Document, store *document.Store) map[string]any {
	progressionURL := documentProgressionURL(d)
	out := map[string]any{"id": d.ID, "title": d.Title, "path": d.Path, "format": d.Format, "status": d.Status, "size": d.Size, "page_count": d.PageCount, "created_at": d.CreatedAt, "updated_at": d.UpdatedAt, "manifest_url": "/opds/v2.0/documents/" + url.PathEscape(d.ID) + "/manifest", "association_source": d.AssociationSource, "progression_url": progressionURL}
	if _, ok := cachedCoverMIME(r.Context(), d, store); ok {
		out["cover_url"] = "/image/documents/" + url.PathEscape(d.ID) + "/thumb"
	}
	if d.GID != nil {
		out["gid"] = *d.GID
		out["token"] = d.Token
	}
	if d.MissingAt != nil {
		out["missing_at"] = d.MissingAt
	}
	return out
}

func documentProgressionURL(d document.Document) string {
	if d.ProgressionTarget == "eh" && d.ProgressionGID > 0 && d.ProgressionToken != "" {
		return fmt.Sprintf("/opds/progression/%d/%s", d.ProgressionGID, url.PathEscape(d.ProgressionToken))
	}
	return "/opds/progression/documents/" + url.PathEscape(d.ID)
}
func documentManifest(w http.ResponseWriter, r *http.Request, store *document.Store, settings config.Settings) {
	d, err := store.Get(r.Context(), r.PathValue("id"))
	if err != nil || d.Status != "available" {
		writeJSON(w, 404, map[string]any{"detail": "document not found"})
		return
	}
	d, _, err = store.EnsurePageIndex(r.Context(), d.ID)
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, document.ErrNotFound) {
			status = http.StatusNotFound
		}
		if errors.Is(err, document.ErrInvalidArchive) {
			status = http.StatusUnprocessableEntity
		}
		writeJSON(w, status, map[string]any{"detail": "document could not be indexed for reading"})
		return
	}
	pub := LocalDocumentPublication(r.Context(), d, store, settings)
	pub["@context"] = feed.RWPMContext
	meta := pub["metadata"].(map[string]any)
	meta["conformsTo"] = []string{feed.DivinaProfile}
	writeBytes(w, 200, feed.MIMEDivina, map[string]string{"Cache-Control": "no-cache"}, ([]byte)(mustJSON(pub)))
}
func BuildDocumentPublication(d document.Document, settings config.Settings) map[string]any {
	builder := feed.Builder{Settings: settings}
	href := builder.Href
	manifest := fmt.Sprintf("/opds/v2.0/documents/%s/manifest", url.PathEscape(d.ID))
	thumb := fmt.Sprintf("/image/documents/%s/thumb", url.PathEscape(d.ID))
	links := []map[string]any{{"rel": "self", "href": href(manifest), "type": feed.MIMEDivina, "title": d.Title}, {"rel": feed.RelThumb, "href": href(thumb), "type": "application/octet-stream", "title": d.Title}, {"rel": feed.RelProgression, "href": href(documentProgressionURL(d)), "type": feed.MIMEProgression}, {"rel": feed.RelStream, "href": href(fmt.Sprintf("/stream/documents/%s/page/{pageNumber}", url.PathEscape(d.ID))), "type": "image/*", "templated": true, "properties": map[string]any{"numberOfPages": d.PageCount}}}
	if d.GID != nil {
		links = append(links, map[string]any{"rel": "alternate", "href": href(fmt.Sprintf("/opds/v2.0/gallery/%d/%s/manifest", *d.GID, url.PathEscape(d.Token))), "type": feed.MIMEDivina})
	}
	reading := make([]map[string]any, 0, d.PageCount)
	for p := 0; p < d.PageCount; p++ {
		number := p + settings.PSEPageBase
		pageURL := fmt.Sprintf("/stream/documents/%s/page/%d", url.PathEscape(d.ID), number)
		reading = append(reading, map[string]any{"href": href(pageURL), "type": "image/*"})
	}
	return map[string]any{"metadata": map[string]any{"title": d.Title, "identifier": "urn:pandaopds:document:" + d.ID, "numberOfPages": d.PageCount, "modified": d.UpdatedAt.UTC().Format(time.RFC3339)}, "links": links, "images": []map[string]string{{"href": href(thumb), "type": "application/octet-stream"}}, "readingOrder": reading}
}

func LocalDocumentPublication(ctx context.Context, d document.Document, store *document.Store, settings config.Settings) map[string]any {
	mimes, err := store.PageMIMEs(ctx, d.ID)
	if latest, e := store.Get(ctx, d.ID); e == nil {
		d = latest
	}
	pub := BuildDocumentPublication(d, settings)
	reading := pub["readingOrder"].([]map[string]any)
	for i := range reading {
		if err == nil && i < len(mimes) {
			reading[i]["type"] = mimes[i]
		}
	}
	if mime, e := store.PageMIMEProfile(ctx, d.ID); e == nil {
		for _, link := range pub["links"].([]map[string]any) {
			if link["rel"] == feed.RelStream {
				link["type"] = mime
			}
		}
	}
	if mime, ok, err := store.CoverMIME(ctx, d.ID); err == nil && ok {
		for _, link := range pub["links"].([]map[string]any) {
			if link["rel"] == feed.RelThumb {
				link["type"] = mime
			}
		}
		pub["images"].([]map[string]string)[0]["type"] = mime
	} else if d.PageCount > 0 {
		if mime, ok, err := store.CachedPageMIME(ctx, d.ID, 0); err == nil && ok {
			for _, link := range pub["links"].([]map[string]any) {
				if link["rel"] == feed.RelThumb {
					link["type"] = mime
				}
			}
			pub["images"].([]map[string]string)[0]["type"] = mime
		}
	}
	return pub
}

func LocalDocumentSummary(ctx context.Context, d document.Document, store *document.Store, settings config.Settings) map[string]any {
	pub := BuildDocumentPublication(d, settings)
	delete(pub, "readingOrder")
	if d.PageCount > 0 {
		if mime, err := store.PageMIMEProfile(ctx, d.ID); err == nil {
			for _, link := range pub["links"].([]map[string]any) {
				if link["rel"] == feed.RelStream {
					link["type"] = mime
				}
			}
		}
	}
	coverMime, hasCover := cachedCoverMIME(ctx, d, store)
	if hasCover {
		for _, link := range pub["links"].([]map[string]any) {
			if link["rel"] == feed.RelThumb {
				link["type"] = coverMime
			}
		}
		pub["images"].([]map[string]string)[0]["type"] = coverMime
	} else {
		// A summary feed must not trigger page indexing or cause clients to
		// fetch every unindexed candidate's cover automatically.
		removeRelation(&pub, feed.RelThumb)
		pub["images"] = []map[string]string{}
	}
	return pub
}

func cachedCoverMIME(ctx context.Context, d document.Document, store *document.Store) (string, bool) {
	if mime, ok, err := store.CoverMIME(ctx, d.ID); err == nil && ok {
		return mime, true
	}
	if mime, ok, err := store.CachedPageMIME(ctx, d.ID, 0); err == nil && ok {
		return mime, true
	}
	return "", false
}

func removeRelation(pub *map[string]any, rel string) {
	links, _ := (*pub)["links"].([]map[string]any)
	out := links[:0]
	for _, link := range links {
		if link["rel"] != rel {
			out = append(out, link)
		}
	}
	(*pub)["links"] = out
}

func DocumentArchiveOPDSFeed(ctx context.Context, store *document.Store, settings config.Settings, page, perPage int) (map[string]any, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 100
	}
	items, total, err := store.List(ctx, page, perPage, "available", "")
	if err != nil {
		return nil, 0, err
	}
	pubs := make([]map[string]any, 0, len(items))
	for _, d := range items {
		pubs = append(pubs, LocalDocumentSummary(ctx, d, store, settings))
	}
	self := "/opds/v2.0/archives"
	next := ""
	if page*perPage < total {
		next = (&feed.Builder{Settings: settings}).Href(fmt.Sprintf("%s?page=%d", self, page+1))
	}
	doc := (&feed.Builder{Settings: settings}).Acquisition("PandaOPDS: Archives", "urn:pandaopds:archives", self, pubs, next, nil)
	return doc, total, nil
}

func DocumentArchiveAtomFeed(ctx context.Context, store *document.Store, settings config.Settings, page, perPage int) ([]byte, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 100
	}
	items, total, err := store.List(ctx, page, perPage, "available", "")
	if err != nil {
		return nil, 0, err
	}
	b := feed.Builder{Settings: settings}
	entries := make([]feed.AtomEntry, 0, len(items))
	for _, d := range items {
		coverMime, hasCover := cachedCoverMIME(ctx, d, store)
		streamMime := "image/*"
		if d.PageCount > 0 {
			if mime, e := store.PageMIMEProfile(ctx, d.ID); e == nil {
				streamMime = mime
			}
		}
		count := d.PageCount
		links := []feed.AtomLink{{Rel: feed.RelAcquisition, Href: b.Href("/opds/v2.0/documents/" + url.PathEscape(d.ID) + "/manifest"), Type: feed.MIMEDivina}, {Rel: feed.RelStream, Href: b.Href("/stream/documents/" + url.PathEscape(d.ID) + "/page/{pageNumber}"), Type: streamMime, Count: &count}, {Rel: feed.RelProgression, Href: b.Href(documentProgressionURL(d)), Type: feed.MIMEProgression}}
		if hasCover {
			links = append([]feed.AtomLink{{Rel: feed.RelThumb, Href: b.Href("/image/documents/" + url.PathEscape(d.ID) + "/thumb"), Type: coverMime, Title: d.Title}}, links...)
		}
		entries = append(entries, feed.AtomEntry{ID: "urn:pandaopds:document:" + d.ID, Title: "Chapter 1: " + d.Title, Updated: d.UpdatedAt.UTC().Format(time.RFC3339), Links: links})
	}
	now := time.Now().UTC().Format(time.RFC3339)
	self := "/opds/v1.2/archives"
	links := []feed.AtomLink{{Rel: "self", Href: b.Href(fmt.Sprintf("%s?page=%d", self, page)), Type: feed.MIMEAtomAcq, Title: "PandaOPDS: Archives"}, {Rel: "start", Href: b.Href("/opds/v1.2"), Type: feed.MIMEAtomNav, Title: "PandaOPDS"}, {Rel: "search", Href: b.Href("/opds/v1.2/search.xml"), Type: feed.MIMEOpenSearch, Title: "Search"}}
	if page*perPage < total {
		links = append(links, feed.AtomLink{Rel: "next", Href: b.Href(fmt.Sprintf("%s?page=%d", self, page+1)), Type: feed.MIMEAtomAcq})
	}
	atom := feed.AtomFeed{DefaultNS: "http://www.w3.org/2005/Atom", PSE: "http://vaemendis.net/opds-pse/ns", OPDS: "http://opds-spec.org/2010/catalog", SearchNS: "http://a9.com/-/spec/opensearch/1.1/", Title: "PandaOPDS: Archives", ID: "urn:pandaopds:archives", Updated: now, Author: feed.AtomAuthor{Name: "PandaOPDS"}, Links: links, Entries: entries}
	raw, e := xml.MarshalIndent(atom, "", "  ")
	if e != nil {
		return nil, 0, e
	}
	return append([]byte(xml.Header), append(raw, '\n')...), total, nil
}
func documentPage(w http.ResponseWriter, r *http.Request, store *document.Store, settings config.Settings) {
	page, err := strconv.Atoi(r.PathValue("page"))
	if err != nil || page < settings.PSEPageBase {
		writeJSON(w, 400, map[string]any{"detail": "invalid page number"})
		return
	}
	body, mime, err := store.OpenPage(r.Context(), r.PathValue("id"), page-settings.PSEPageBase)
	if err != nil {
		if errors.Is(err, document.ErrNotFound) {
			writeJSON(w, 404, map[string]any{"detail": "page not found"})
		} else {
			writeJSON(w, 409, map[string]any{"detail": "document changed or cannot be read"})
		}
		return
	}
	writeBytes(w, 200, mime, map[string]string{"Cache-Control": "no-cache"}, body)
}

func documentCover(w http.ResponseWriter, r *http.Request, store *document.Store) {
	if data, mime, ok, err := store.Cover(r.Context(), r.PathValue("id")); err == nil && ok {
		writeBytes(w, 200, mime, map[string]string{"Cache-Control": "no-cache"}, data)
		return
	}
	body, mime, err := store.OpenPage(r.Context(), r.PathValue("id"), 0)
	if err != nil {
		writeJSON(w, 404, map[string]any{"detail": "document cover not found"})
		return
	}
	writeBytes(w, 200, mime, map[string]string{"Cache-Control": "no-cache"}, body)
}

func documentProgression(w http.ResponseWriter, r *http.Request, store *document.Store) {
	id := r.PathValue("id")
	if _, err := store.Get(r.Context(), id); err != nil {
		writeJSON(w, 404, map[string]any{"detail": "document not found"})
		return
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		var modified string
		var device, refs string
		var value float64
		var title sql.NullString
		err := store.DB().QueryRowContext(r.Context(), `SELECT modified,device_json,progression,title,references_json FROM progressions WHERE target_type='document' AND target_id=?`, id).Scan(&modified, &device, &value, &title, &refs)
		if err == sql.ErrNoRows {
			if r.Method == http.MethodHead {
				w.WriteHeader(200)
			} else {
				w.Header().Set("Content-Type", "application/opds-progression+json")
				w.WriteHeader(200)
			}
			return
		}
		if err != nil {
			writeJSON(w, 500, map[string]any{"detail": "could not read progression"})
			return
		}
		var dev any
		var references []string
		_ = json.Unmarshal([]byte(device), &dev)
		_ = json.Unmarshal([]byte(refs), &references)
		out := map[string]any{"modified": modified, "device": dev, "progression": value, "references": references}
		if title.Valid {
			out["title"] = title.String
		}
		writeBytes(w, 200, "application/opds-progression+json", nil, []byte(mustJSON(out)))
		return
	}
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", "GET, PUT, HEAD")
		writeJSON(w, 405, map[string]any{"detail": "method not allowed"})
		return
	}
	const maxBody = 1 << 20
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil || len(body) == 0 || len(body) > maxBody {
		writeJSON(w, 400, map[string]any{"detail": "invalid progression"})
		return
	}
	var input struct {
		Modified string `json:"modified"`
		Device   struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"device"`
		Progression *float64 `json:"progression"`
		Title       *string  `json:"title,omitempty"`
		References  []string `json:"references"`
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(body, &raw) != nil || json.Unmarshal(body, &input) != nil || input.Progression == nil || *input.Progression < 0 || *input.Progression > 1 || math.IsNaN(*input.Progression) || math.IsInf(*input.Progression, 0) || input.Device.ID == "" || strings.TrimSpace(input.Device.Name) != input.Device.Name || input.Device.Name == "" {
		writeJSON(w, 400, map[string]any{"detail": "invalid progression"})
		return
	}
	if title, ok := raw["title"]; ok && string(title) == "null" {
		writeJSON(w, 400, map[string]any{"detail": "invalid progression"})
		return
	}
	if refs, ok := raw["references"]; ok {
		if string(refs) == "null" {
			writeJSON(w, 400, map[string]any{"detail": "invalid progression"})
			return
		}
		var values []json.RawMessage
		if json.Unmarshal(refs, &values) != nil {
			writeJSON(w, 400, map[string]any{"detail": "invalid progression"})
			return
		}
		for _, v := range values {
			var ref string
			if string(v) == "null" || json.Unmarshal(v, &ref) != nil {
				writeJSON(w, 400, map[string]any{"detail": "invalid progression"})
				return
			}
		}
	}
	modified, err := time.Parse(time.RFC3339Nano, input.Modified)
	u, ue := url.Parse(input.Device.ID)
	if err != nil || ue != nil || !u.IsAbs() {
		writeJSON(w, 400, map[string]any{"detail": "invalid progression"})
		return
	}
	for _, ref := range input.References {
		if strings.TrimSpace(ref) != ref {
			writeJSON(w, 400, map[string]any{"detail": "invalid progression"})
			return
		}
		if _, e := url.Parse(ref); e != nil {
			writeJSON(w, 400, map[string]any{"detail": "invalid progression"})
			return
		}
	}
	devBytes, _ := json.Marshal(input.Device)
	refsBytes, _ := json.Marshal(input.References)
	if len(refsBytes) == 0 {
		refsBytes = []byte("[]")
	}
	now := time.Now().UnixNano()
	tx, err := store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, 500, map[string]any{"detail": "could not save progression"})
		return
	}
	defer tx.Rollback()
	var oldModified string
	var oldDevice, oldRefs string
	var oldValue float64
	var oldTitle sql.NullString
	err = tx.QueryRowContext(r.Context(), `SELECT modified,device_json,progression,title,references_json FROM progressions WHERE target_type='document' AND target_id=?`, id).Scan(&oldModified, &oldDevice, &oldValue, &oldTitle, &oldRefs)
	status := 201
	if err == nil {
		status = 200
		oldTime, _ := time.Parse(time.RFC3339Nano, oldModified)
		if modified.Before(oldTime) {
			writeJSON(w, 409, map[string]any{"detail": "newer progression already exists"})
			return
		}
		if modified.Equal(oldTime) {
			old := map[string]any{"modified": oldModified, "device": json.RawMessage(oldDevice), "progression": oldValue, "references": json.RawMessage(oldRefs)}
			if oldTitle.Valid {
				old["title"] = oldTitle.String
			}
			newPayload := map[string]any{"modified": modified.UTC().Format(time.RFC3339Nano), "device": json.RawMessage(devBytes), "progression": *input.Progression, "references": json.RawMessage(refsBytes)}
			if input.Title != nil {
				newPayload["title"] = *input.Title
			}
			a, _ := json.Marshal(old)
			b, _ := json.Marshal(newPayload)
			if string(a) != string(b) {
				writeJSON(w, 409, map[string]any{"detail": "equal-time progression conflicts"})
				return
			}
			status = 200
			out := map[string]any{"modified": oldModified, "device": json.RawMessage(oldDevice), "progression": oldValue, "references": json.RawMessage(oldRefs)}
			if oldTitle.Valid {
				out["title"] = oldTitle.String
			}
			writeBytes(w, status, "application/opds-progression+json", nil, []byte(mustJSON(out)))
			return
		}
	}
	if err != nil && err != sql.ErrNoRows {
		writeJSON(w, 500, map[string]any{"detail": "could not read progression"})
		return
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO progressions(target_type,target_id,modified,device_json,progression,title,references_json,written) VALUES('document',?,?,?,?,?,?,?) ON CONFLICT(target_type,target_id) DO UPDATE SET modified=excluded.modified,device_json=excluded.device_json,progression=excluded.progression,title=excluded.title,references_json=excluded.references_json,written=excluded.written`, id, modified.UTC().Format(time.RFC3339Nano), string(devBytes), *input.Progression, input.Title, string(refsBytes), now); err != nil {
		writeJSON(w, 500, map[string]any{"detail": "could not save progression"})
		return
	}
	if err = tx.Commit(); err != nil {
		writeJSON(w, 500, map[string]any{"detail": "could not save progression"})
		return
	}
	out := map[string]any{"modified": modified.UTC().Format(time.RFC3339Nano), "device": input.Device, "progression": *input.Progression, "references": input.References}
	if input.Title != nil {
		out["title"] = *input.Title
	}
	writeBytes(w, status, "application/opds-progression+json", nil, []byte(mustJSON(out)))
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
