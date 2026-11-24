package progression

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"pandaopds/internal/eh"
)

func TestProgressionPersistsMergesAliasAndRejectsStaleWrite(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	doc := Document{Modified: base, Device: Device{ID: "urn:uuid:device", Name: "Reader"}, Progression: .8, References: []string{"page8"}}
	if status, _, err := s.Put(10, "old", doc); err != nil || status != 201 {
		t.Fatalf("first put=(%d,%v)", status, err)
	}
	if err := s.Observe(10, "old", 20, "new"); err != nil {
		t.Fatal(err)
	}
	next := doc
	next.Modified = base.Add(time.Minute)
	next.Progression = .2
	if status, _, err := s.Put(20, "new", next); err != nil || status != 200 {
		t.Fatalf("newer lower progression=(%d,%v)", status, err)
	}
	stale := next
	stale.Modified = base
	if _, _, err := s.Put(10, "old", stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale update error=%v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Get(10, "old")
	if err != nil || len(got) != 1 || got[0].Progression != .2 {
		t.Fatalf("persisted progression=%#v, err=%v", got, err)
	}
}

func TestImportBoltCopiesProgressionsAliasesSnapshotsAndProtectionOnce(t *testing.T) {
	dir := t.TempDir()
	legacy := dir + "/old.db"
	b, err := bolt.Open(legacy, 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	var rawFixture []byte
	err = b.Update(func(tx *bolt.Tx) error {
		bucket, e := tx.CreateBucket([]byte("progression-v1"))
		if e != nil {
			return e
		}
		x := stored{GID: 9, Token: "tok", Written: 77, Doc: Document{Modified: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Device: Device{ID: "dev", Name: "Reader"}, Progression: .4, References: []string{"page4"}}, Snapshot: &Snapshot{Title: "Work", Category: "Manga"}}
		raw, _ := json.Marshal(x)
		var payload map[string]any
		_ = json.Unmarshal(raw, &payload)
		payload["future_field"] = map[string]any{"nested": []any{"keep", float64(7)}}
		raw, _ = json.Marshal(payload)
		rawFixture = append([]byte(nil), raw...)
		if e = bucket.Put([]byte("r/9/tok"), raw); e != nil {
			return e
		}
		if e = bucket.Put([]byte("a/8/old"), []byte("9/tok")); e != nil {
			return e
		}
		return bucket.Put([]byte("p/9/tok"), []byte{1})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.ImportBolt(legacy); err != nil {
		t.Fatal(err)
	}
	var preserved []byte
	if err = s.db.QueryRow(`SELECT legacy_raw_json FROM progressions WHERE target_type='eh' AND target_id='9/tok'`).Scan(&preserved); err != nil {
		t.Fatal(err)
	}
	if string(preserved) != string(rawFixture) {
		t.Fatalf("legacy raw record changed: %s", preserved)
	}
	got, err := s.Get(8, "old")
	if err != nil || len(got) != 1 || got[0].Progression != .4 {
		t.Fatalf("imported progression=%#v err=%v", got, err)
	}
	if err = s.SetSnapshot(9, "tok", Snapshot{Title: "Updated", Category: "Drama"}); err != nil {
		t.Fatal(err)
	}
	if err = s.ImportBolt(legacy); err != nil {
		t.Fatal(err)
	}
	items, total, err := s.ListHistoryQuery(HistoryQuery{Offset: 0, Limit: 10, Category: "Drama"})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("SQL filtered history=%#v total=%d err=%v", items, total, err)
	}
	var count int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM legacy_imports WHERE name='bbolt_progression'`).Scan(&count)
	if count != 1 {
		t.Fatalf("import marker count=%d", count)
	}
}

func TestEqualTimestampDifferentPayloadConflictsButRetryIsIdempotent(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	d := Document{Modified: time.Now().UTC(), Device: Device{ID: "https://reader.example", Name: "Reader"}, Progression: .5}
	if _, _, err = s.Put(1, "tok", d); err != nil {
		t.Fatal(err)
	}
	if status, _, err := s.Put(1, "tok", d); err != nil || status != 200 {
		t.Fatalf("retry=(%d,%v)", status, err)
	}
	d.Progression = .6
	if _, _, err = s.Put(1, "tok", d); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict error=%v", err)
	}
}

func TestObserveTrimsOnlyExcessOrphanAliasesAndKeepsProgressTargets(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<20005) INSERT INTO progression_aliases(alias,target_id,target_type) SELECT 'orphan/'||x,'100/'||x,'eh' FROM n`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(`INSERT INTO progressions(target_type,target_id,gid,token,modified,device_json,progression,references_json,written)VALUES('eh','55/keep',55,'keep','2026-01-01T00:00:00Z','{}',0.5,'[]',1)`)
	if err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"55/keep", "56/keep", "57/keep"} {
		if _, err = tx.Exec(`INSERT INTO progression_aliases(alias,target_id,target_type)VALUES(?,?,'eh')`, alias, "55/keep"); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = s.Observe(1, "new", 1, "new"); err != nil {
		t.Fatal(err)
	}
	var orphans, protected int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM progression_aliases a WHERE a.target_type='eh' AND NOT EXISTS(SELECT 1 FROM progressions p WHERE p.target_type=a.target_type AND p.target_id=a.target_id)`).Scan(&orphans); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM progression_aliases WHERE alias IN ('55/keep','56/keep','57/keep')`).Scan(&protected); err != nil {
		t.Fatal(err)
	}
	if orphans != 20000 || protected != 3 {
		t.Fatalf("orphans=%d protected aliases=%d", orphans, protected)
	}
}

func TestObserveTransitiveAndCanonicalReobservationPreservesRecord(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	d := Document{Modified: time.Now().UTC(), Device: Device{ID: "urn:uuid:a", Name: "A"}, Progression: .4, References: []string{"page4"}}
	if _, _, err = s.Put(1, "a", d); err != nil {
		t.Fatal(err)
	}
	if err = s.Observe(1, "a", 2, "b"); err != nil {
		t.Fatal(err)
	}
	if err = s.Observe(2, "b", 3, "c"); err != nil {
		t.Fatal(err)
	}
	if err = s.Observe(3, "c", 3, "c"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(1, "a")
	if err != nil || len(got) != 1 || len(got[0].References) != 1 {
		t.Fatalf("old alias data=%#v err=%v", got, err)
	}
	got, err = s.Get(3, "c")
	if err != nil || len(got) != 1 || len(got[0].References) != 0 {
		t.Fatalf("cross-version data=%#v err=%v", got, err)
	}
}

func TestConcurrentOlderAndNewerWritesKeepNewest(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	older := Document{Modified: base, Device: Device{ID: "urn:uuid:old", Name: "Old"}, Progression: .8}
	newer := Document{Modified: base.Add(time.Second), Device: Device{ID: "urn:uuid:new", Name: "New"}, Progression: .2}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, d := range []Document{older, newer} {
		wg.Add(1)
		go func(d Document) {
			defer wg.Done()
			<-start
			_, _, e := s.Put(44, "tok", d)
			if e != nil && !errors.Is(e, ErrConflict) {
				errs <- e
			}
		}(d)
	}
	close(start)
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	got, e := s.Get(44, "tok")
	if e != nil || len(got) != 1 || !got[0].Modified.Equal(newer.Modified) {
		t.Fatalf("concurrent result=%#v err=%v", got, e)
	}
}

func TestCapEvictsOldestUnarchivedAndKeepsArchivedWork(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= MaxWorks+2; i++ {
		_, err = tx.Exec(`INSERT INTO progressions(target_type,target_id,gid,token,modified,device_json,progression,references_json,written) VALUES('eh',?,?,?,?,?,0,'[]',?)`, identity(i, "t"), i, "t", time.Unix(int64(i), 0).UTC().Format(time.RFC3339Nano), `{"id":"urn:uuid:x","name":"x"}`, i)
		if err != nil {
			t.Fatal(err)
		}
	}
	err = tx.Commit()
	if err != nil {
		t.Fatal(err)
	}
	group := Group{Active: Identity{GID: MaxWorks + 1, Token: "t"}, Identities: []Identity{{GID: 1, Token: "t"}, {GID: MaxWorks + 1, Token: "t"}}, Available: []Identity{{GID: MaxWorks + 1, Token: "t"}}}
	if err = s.ReconcileArchives([]Group{group}); err != nil {
		t.Fatal(err)
	}
	newDoc := Document{Modified: time.Now().UTC(), Device: Device{ID: "urn:uuid:new", Name: "new"}, Progression: .1}
	if _, _, err = s.Put(MaxWorks+3, "t", newDoc); err != nil {
		t.Fatal(err)
	}
	if docs, err := s.Get(1, "t"); err != nil || len(docs) != 1 || !docs[0].Modified.Equal(time.Unix(MaxWorks+1, 0)) {
		t.Fatalf("protected archive record not retained: %#v err=%v", docs, err)
	}
	if docs, _ := s.Get(2, "t"); len(docs) != 0 {
		t.Fatalf("oldest unarchived work was retained: %#v", docs)
	}
	if docs, _ := s.Get(MaxWorks+3, "t"); len(docs) != 1 {
		t.Fatalf("new work missing: %#v", docs)
	}
	if count, _ := s.Count(); count != MaxWorks+1 {
		t.Fatalf("record count=%d want %d including protected archive", count, MaxWorks+1)
	}
	group.Available = nil
	if err = s.ReconcileArchives([]Group{group}); err != nil {
		t.Fatal(err)
	}
	if docs, _ := s.Get(3, "t"); len(docs) != 0 {
		t.Fatalf("demoted work should have been pruned at cap: %#v", docs)
	}
	if docs, _ := s.Get(1, "t"); len(docs) != 1 {
		t.Fatalf("former archive record was lost during demotion: %#v", docs)
	}
}

func TestListHistoryAndSnapshot(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Put 3 items with different timestamps
	t1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	doc1 := Document{Modified: t1, Device: Device{ID: "urn:uuid:dev", Name: "Reader"}, Progression: 0.1}
	doc2 := Document{Modified: t2, Device: Device{ID: "urn:uuid:dev", Name: "Reader"}, Progression: 0.5}
	doc3 := Document{Modified: t3, Device: Device{ID: "urn:uuid:dev", Name: "Reader"}, Progression: 0.9}

	if _, _, err := s.Put(101, "tok1", doc1); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, _, err := s.Put(102, "tok2", doc2); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, _, err := s.Put(103, "tok3", doc3); err != nil {
		t.Fatal(err)
	}

	// Set snapshot on item 102
	snap := Snapshot{
		Title:    "Gallery Two",
		Category: "Manga",
		Tags:     []eh.GalleryTag{{Namespace: "artist", Key: "alice", Status: "confidence"}},
		CoverURL: "https://example.com/cover2.jpg",
		Rating:   4.5,
	}
	if err := s.SetSnapshot(102, "tok2", snap); err != nil {
		t.Fatal(err)
	}

	// List history: should be ordered by recent write (103, 102, 101)
	items, total, err := s.ListHistory(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("expected total 3, got %d", total)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[0].GID != 103 || items[1].GID != 102 || items[2].GID != 101 {
		t.Fatalf("items out of order: %d, %d, %d", items[0].GID, items[1].GID, items[2].GID)
	}
	if items[1].Snapshot == nil || items[1].Snapshot.Title != "Gallery Two" || len(items[1].Snapshot.Tags) != 1 || items[1].Snapshot.Tags[0].String() != "artist:alice" {
		t.Fatalf("snapshot not attached or incorrect: %#v", items[1].Snapshot)
	}
	if err := s.SetSnapshot(101, "tok1", Snapshot{Title: "Old snapshot"}); err != nil {
		t.Fatal(err)
	}
	legacy, _, err := s.ListHistory(2, 1)
	if err != nil || len(legacy) != 1 || legacy[0].Snapshot == nil || len(legacy[0].Snapshot.Tags) != 0 {
		t.Fatalf("legacy snapshot without tags should remain readable: %#v err=%v", legacy, err)
	}

	// Pagination test
	p2, total2, err := s.ListHistory(1, 1)
	if err != nil || total2 != 3 || len(p2) != 1 || p2[0].GID != 102 {
		t.Fatalf("pagination failed: total=%d, items=%#v, err=%v", total2, p2, err)
	}
}

func TestMatchReadingStatusAndFilteredHistory(t *testing.T) {
	// 1. Boundary unit tests for MatchReadingStatus
	cases := []struct {
		hasRecord bool
		prog      float64
		filter    string
		want      bool
	}{
		{false, 0.0, "unread", true},
		{true, 0.0, "unread", true},
		{true, 0.009, "unread", true},
		{true, 0.01, "unread", false},
		{true, 0.01, "reading", true},
		{true, 0.5, "reading", true},
		{true, 0.949, "reading", true},
		{true, 0.95, "reading", false},
		{true, 0.95, "completed", true},
		{true, 0.98, "completed", true},
		{true, 1.0, "completed", true},
		{false, 0.0, "completed", false},
		{true, 0.5, "", true},
	}
	for i, tc := range cases {
		got := MatchReadingStatus(tc.hasRecord, tc.prog, tc.filter)
		if got != tc.want {
			t.Errorf("case %d MatchReadingStatus(%v, %v, %q) = %v, want %v", i, tc.hasRecord, tc.prog, tc.filter, got, tc.want)
		}
	}

	// 2. Integration with store
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now().UTC()
	// GID 1: Unread (0.005)
	_, _, _ = s.Put(1, "tok1", Document{Modified: now.Add(time.Second), Device: Device{ID: "urn:uuid:dev", Name: "R"}, Progression: 0.005})
	// GID 2: Reading (0.5)
	_, _, _ = s.Put(2, "tok2", Document{Modified: now.Add(2 * time.Second), Device: Device{ID: "urn:uuid:dev", Name: "R"}, Progression: 0.5})
	// GID 3: Completed (0.96)
	_, _, _ = s.Put(3, "tok3", Document{Modified: now.Add(3 * time.Second), Device: Device{ID: "urn:uuid:dev", Name: "R"}, Progression: 0.96})

	// Filter unread
	unreadItems, unreadTotal, err := s.ListHistoryFiltered(0, 10, func(it HistoryItem) bool {
		return MatchReadingStatus(true, it.Doc.Progression, "unread")
	})
	if err != nil || unreadTotal != 1 || len(unreadItems) != 1 || unreadItems[0].GID != 1 {
		t.Fatalf("filter unread failed: total=%d, items=%+v, err=%v", unreadTotal, unreadItems, err)
	}

	// Filter reading
	readingItems, readingTotal, err := s.ListHistoryFiltered(0, 10, func(it HistoryItem) bool {
		return MatchReadingStatus(true, it.Doc.Progression, "reading")
	})
	if err != nil || readingTotal != 1 || len(readingItems) != 1 || readingItems[0].GID != 2 {
		t.Fatalf("filter reading failed: total=%d, items=%+v, err=%v", readingTotal, readingItems, err)
	}

	// Filter completed
	completedItems, completedTotal, err := s.ListHistoryFiltered(0, 10, func(it HistoryItem) bool {
		return MatchReadingStatus(true, it.Doc.Progression, "completed")
	})
	if err != nil || completedTotal != 1 || len(completedItems) != 1 || completedItems[0].GID != 3 {
		t.Fatalf("filter completed failed: total=%d, items=%+v, err=%v", completedTotal, completedItems, err)
	}

	// Test GetProgressions
	progs, err := s.GetProgressions()
	if err != nil {
		t.Fatal(err)
	}
	if progs["1/tok1"] != 0.005 || progs["2/tok2"] != 0.5 || progs["3/tok3"] != 0.96 {
		t.Fatalf("unexpected progressions snapshot: %+v", progs)
	}
}
