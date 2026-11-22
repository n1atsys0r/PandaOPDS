package subscription

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"pandaopds/internal/eh"
	"pandaopds/internal/storage"
)

func testStore(t *testing.T) (*storage.DB, *Store) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, New(db.DB)
}

func TestAddPagePersistsBaselineMembershipAndLocalFilters(t *testing.T) {
	ctx := context.Background()
	db, s := testStore(t)
	a, err := s.Create(ctx, Config{Name: "A", Source: "search", Query: map[string]any{"keywords": "cat"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create(ctx, Config{Name: "B", Source: "search", Query: map[string]any{"keywords": "dog"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO subscription_runs(id,subscription_id,started_at,status,revision)VALUES('run-a',?, '2026-01-01T00:00:00Z','running',1)`, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO subscription_runs(id,subscription_id,started_at,status,revision)VALUES('run-b',?, '2026-01-01T00:00:00Z','running',1)`, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	favcat := 2
	detailed := `{"title":"Detailed Cat","title_jpn":"猫","category":"Manga","uploader":"artist","cover_url":"https://ehgt.org/cover.jpg","page_count":12,"rating":4.5,"publish_time":"2026-01-01","language":"en","tags":{"artist":[{"namespace":"artist","key":"artist:name"}]}}`
	_, err = db.Exec(`INSERT INTO publications(provider,gid,token,metadata_json,title,title_jpn,category,uploader,cover_url,page_count,rating,publish_time,language,tags_json,metadata_level,created_at,updated_at)VALUES('eh',42,'abcd',?,?,?,?,?,?,?,?,?,?,?,2,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, detailed, "Detailed Cat", "猫", "Manga", "artist", "https://ehgt.org/cover.jpg", 12, 4.5, "2026-01-01", "en", `{"artist":[{"namespace":"artist","key":"artist:name"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	gallery := eh.GalleryListItem{GID: 42, Token: "AbCd", Title: "Cat title", Category: "Manga", Uploader: "artist", Favcat: &favcat}
	if _, err = s.AddPage(ctx, a.ID, "run-a", 1, 1, "", false, true, []eh.GalleryListItem{gallery}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AddPage(ctx, b.ID, "run-b", 1, 1, "", false, false, []eh.GalleryListItem{gallery}); err != nil {
		t.Fatal(err)
	}
	items, total, err := s.ListItems(ctx, ItemQuery{SubscriptionIDs: []string{a.ID, b.ID}, Q: "cat", Category: "Manga", Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("deduped list total=%d items=%d", total, len(items))
	}
	if items[0]["inspected"] != false || items[0]["title"] != "Detailed Cat" || items[0]["title_jpn"] != "猫" {
		t.Fatalf("item=%#v", items[0])
	}
	if _, ok := items[0]["tags"].(map[string]any); !ok {
		t.Fatalf("list metadata replaced detailed tags: %#v", items[0]["tags"])
	}
	members := items[0]["membership_ids"].([]string)
	if len(members) != 2 {
		t.Fatalf("membership_ids=%v", members)
	}
	var persisted int
	if err := db.QueryRow(`SELECT favcat FROM subscription_items WHERE subscription_id=?`, a.ID).Scan(&persisted); err != nil || persisted != 2 {
		t.Fatalf("favcat=%d err=%v", persisted, err)
	}
	filtered, n, err := s.ListItems(ctx, ItemQuery{SubscriptionIDs: []string{a.ID, b.ID}, Q: "missing", Page: 1, PerPage: 20})
	if err != nil || n != 0 || len(filtered) != 0 {
		t.Fatalf("filtered=%v total=%d err=%v", filtered, n, err)
	}
}

func TestAddPageRejectsOldRevisionAndDoesNotCreateRows(t *testing.T) {
	ctx := context.Background()
	db, s := testStore(t)
	c, err := s.Create(ctx, Config{Name: "revision", Source: "search", Query: map[string]any{"keywords": "old"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO subscription_runs(id,subscription_id,started_at,status,revision)VALUES('old-run',?, '2026-01-01T00:00:00Z','running',1)`, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	c.Query = map[string]any{"keywords": "new"}
	if _, err = s.Update(ctx, c.ID, c); err != nil {
		t.Fatal(err)
	}
	_, err = s.AddPage(ctx, c.ID, "old-run", 1, 1, "", false, true, []eh.GalleryListItem{{GID: 55, Token: "token", Title: "stale"}})
	if err != ErrStaleRevision {
		t.Fatalf("AddPage err=%v", err)
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM publications WHERE gid=55`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("stale rows=%d err=%v", count, err)
	}
}

func TestLegacyFavoritesSubscriptionMigratesToOrdinaryPreset(t *testing.T) {
	ctx := context.Background()
	db, s := testStore(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`INSERT INTO subscriptions(id,name,source,query_json,interval_seconds,enabled,auto_download,download_rules_json,revision,baseline_complete,created_at,updated_at) VALUES('builtin-favorites','EH 收藏','favorites','{"favorite_categories":[3],"categories":[2],"max_pages":50}',3600,1,1,'{"quality":"res","categories":[4]}',7,1,?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO publications(provider,gid,token,metadata_json,created_at,updated_at) VALUES('eh',123,'abcd','{}',?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO subscription_items(subscription_id,publication_id,first_seen_at,last_seen_at,remote_member,first_seen_revision,baseline_item,pending_auto,favcat) SELECT 'builtin-favorites',id,?,?,1,6,1,0,3 FROM publications WHERE gid=123`, now, now)
	if err != nil {
		t.Fatal(err)
	}

	disabled, err := s.MigrateLegacyFavoriteSubscriptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(disabled) != 1 || disabled[0] != "builtin-favorites" {
		t.Fatalf("subscriptions with legacy folder archive rules=%v", disabled)
	}
	c, err := s.Get(ctx, "builtin-favorites")
	if err != nil {
		t.Fatal(err)
	}
	if c.Source != "preset" || c.Query["preset"] != "favorites:3" || intNumber(c.Query["max_pages"]) != 1 || c.BaselineComplete != true || c.Revision != 8 {
		t.Fatalf("migrated subscription=%#v", c)
	}
	if c.Query["categories"] == nil || c.AutoDownload || c.DownloadRules["quality"] != "res" || c.DownloadRules["categories"] != nil {
		t.Fatalf("migration lost gallery filter or retained unsafe auto archive: %#v", c)
	}
	var itemCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM subscription_items WHERE subscription_id='builtin-favorites'`).Scan(&itemCount); err != nil || itemCount != 1 {
		t.Fatalf("cached items=%d err=%v", itemCount, err)
	}
	members, err := s.FavoritePresetMemberships(ctx)
	if err != nil || members["123:abcd"] != 3 {
		t.Fatalf("last-seen favorites=%v err=%v", members, err)
	}
	if err := s.Delete(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.MigrateLegacyFavoriteSubscriptions(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, c.ID); err == nil {
		t.Fatal("deleted subscription was recreated")
	}
}

func TestCleanupKeepsDedupTombstoneAcrossRepeatedFetches(t *testing.T) {
	ctx := context.Background()
	_, s := testStore(t)
	c, err := s.Create(ctx, Config{Name: "cleanup", Source: "search", Query: map[string]any{"keywords": "fox"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`INSERT INTO subscription_runs(id,subscription_id,started_at,status,revision) VALUES('cleanup-run',?,?,'running',1)`, c.ID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	g := eh.GalleryListItem{GID: 987, Token: "aa987", Title: "Fox"}
	if _, err = s.AddPage(ctx, c.ID, "cleanup-run", 1, 1, "", false, false, []eh.GalleryListItem{g}); err != nil {
		t.Fatal(err)
	}
	inspected := true
	if err = s.MarkItems(ctx, []string{c.ID}, g.GID, g.Token, &inspected, nil); err != nil {
		t.Fatal(err)
	}
	removed, err := s.CleanupItems(ctx, c.ID, "inspected")
	if err != nil || removed != 1 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	if _, err = s.AddPage(ctx, c.ID, "cleanup-run", 1, 2, "", false, false, []eh.GalleryListItem{g}); err != nil {
		t.Fatal(err)
	}
	items, total, err := s.ListItems(ctx, ItemQuery{SubscriptionIDs: []string{c.ID}, Page: 1, PerPage: 20})
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("cleaned item returned: items=%#v total=%d err=%v", items, total, err)
	}
	var purged, pending int
	if err = s.db.QueryRow(`SELECT purged,pending_auto FROM subscription_items WHERE subscription_id=?`, c.ID).Scan(&purged, &pending); err != nil || purged != 1 || pending != 0 {
		t.Fatalf("tombstone purged=%d pending=%d err=%v", purged, pending, err)
	}
}
