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
