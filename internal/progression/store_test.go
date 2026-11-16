package progression

import (
	"encoding/json"
	"errors"
	bolt "go.etcd.io/bbolt"
	"sync"
	"testing"
	"time"
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
	err = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		for i := 1; i <= MaxWorks+2; i++ {
			x := stored{GID: i, Token: "t", Doc: Document{Modified: time.Unix(int64(i), 0), Device: Device{ID: "urn:uuid:x", Name: "x"}}, Written: int64(i)}
			raw, _ := json.Marshal(x)
			if e := b.Put(recKey(i, "t"), raw); e != nil {
				return e
			}
			if e := b.Put(lruKey(x.Written, identity(i, "t")), []byte{}); e != nil {
				return e
			}
		}
		return nil
	})
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
	if items[1].Snapshot == nil || items[1].Snapshot.Title != "Gallery Two" {
		t.Fatalf("snapshot not attached or incorrect: %#v", items[1].Snapshot)
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
