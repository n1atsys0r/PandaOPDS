package progression

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

const bucket = "progression-v1"
const MaxWorks = 10000

var ErrConflict = errors.New("progression conflicts with newer or equal-time state")
var ErrNotFound = errors.New("progression not found")

type Device struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Document struct {
	Modified    time.Time `json:"modified"`
	Device      Device    `json:"device"`
	Progression float64   `json:"progression"`
	Title       *string   `json:"title,omitempty"`
	References  []string  `json:"references,omitempty"`
}
type stored struct {
	GID     int      `json:"gid"`
	Token   string   `json:"token"`
	Doc     Document `json:"doc"`
	Written int64    `json:"written"`
}
type Store struct{ db *bolt.DB }

func Open(stateDir string) (*Store, error) {
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return nil, err
	}
	db, err := bolt.Open(filepath.Join(stateDir, "opds-progression.db"), 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	s := &Store{db}
	err = db.Update(func(tx *bolt.Tx) error { _, e := tx.CreateBucketIfNotExists([]byte(bucket)); return e })
	if err != nil {
		db.Close()
		return nil, err
	}
	if err := db.Update(func(tx *bolt.Tx) error { return rebuildLRUIndex(tx.Bucket([]byte(bucket))) }); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error               { return s.db.Close() }
func identity(gid int, token string) string { return strconv.Itoa(gid) + "/" + strings.ToLower(token) }
func recKey(gid int, token string) []byte {
	return []byte("r/" + identity(gid, token))
}
func lruKey(written int64, id string) []byte { return []byte(fmt.Sprintf("u/%020d/%s", written, id)) }
func rebuildLRUIndex(b *bolt.Bucket) error {
	keys := [][]byte{}
	c := b.Cursor()
	for k, _ := c.Seek([]byte("u/")); k != nil && strings.HasPrefix(string(k), "u/"); k, _ = c.Next() {
		keys = append(keys, append([]byte(nil), k...))
	}
	for _, k := range keys {
		if err := b.Delete(k); err != nil {
			return err
		}
	}
	c = b.Cursor()
	for k, v := c.Seek([]byte("r/")); k != nil && strings.HasPrefix(string(k), "r/"); k, v = c.Next() {
		var x stored
		if err := json.Unmarshal(v, &x); err != nil {
			return err
		}
		id := strings.TrimPrefix(string(k), "r/")
		if err := b.Put(lruKey(x.Written, id), []byte{}); err != nil {
			return err
		}
	}
	return nil
}
func aliasKey(gid int, token string) []byte { return []byte("a/" + identity(gid, token)) }
func readAlias(b *bolt.Bucket, gid int, token string) (int, string) {
	v := b.Get(aliasKey(gid, token))
	if v == nil {
		return gid, token
	}
	p := strings.SplitN(string(v), "/", 2)
	if len(p) != 2 {
		return gid, token
	}
	n, e := strconv.Atoi(p[0])
	if e != nil {
		return gid, token
	}
	return n, p[1]
}
func (s *Store) Observe(gid int, token string, activeGID int, activeToken string) error {
	if gid < 1 || token == "" || activeGID < 1 || activeToken == "" {
		return errors.New("invalid gallery identity")
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if err := observeBucket(b, gid, token, activeGID, activeToken); err != nil {
			return err
		}
		return trimAliases(b)
	})
}
func observeBucket(b *bolt.Bucket, gid int, token string, activeGID int, activeToken string) error {
	token, activeToken = strings.ToLower(token), strings.ToLower(activeToken)
	if gid < 1 || token == "" || activeGID < 1 || activeToken == "" {
		return errors.New("invalid gallery identity")
	}
	oldG, oldT := readAlias(b, gid, token)
	newG, newT := readAlias(b, activeGID, activeToken)
	if oldG == newG && oldT == newT {
		return nil
	}
	oldIdentity, newIdentity := identity(oldG, oldT), identity(newG, newT)
	oldKey, newKey := recKey(oldG, oldT), recKey(newG, newT)
	oldRaw, newRaw := b.Get(oldKey), b.Get(newKey)
	var chosen *stored
	if oldRaw != nil && (newRaw == nil || prefer(oldRaw, newRaw)) {
		var x stored
		if err := json.Unmarshal(oldRaw, &x); err != nil {
			return err
		}
		chosen = &x
	} else if newRaw != nil {
		var x stored
		if err := json.Unmarshal(newRaw, &x); err != nil {
			return err
		}
		chosen = &x
	}
	for i, raw := range [][]byte{oldRaw, newRaw} {
		if raw == nil {
			continue
		}
		var x stored
		if err := json.Unmarshal(raw, &x); err != nil {
			return err
		}
		idx := oldIdentity
		if i == 1 {
			idx = newIdentity
		}
		if err := b.Delete(lruKey(x.Written, idx)); err != nil {
			return err
		}
	}
	if chosen != nil {
		merged, err := json.Marshal(chosen)
		if err != nil {
			return err
		}
		if err = b.Put(newKey, merged); err != nil {
			return err
		}
		if err = b.Put(lruKey(chosen.Written, newIdentity), []byte{}); err != nil {
			return err
		}
	}
	if err := b.Delete(oldKey); err != nil {
		return err
	}
	oldProtected := b.Get([]byte("p/"+oldIdentity)) != nil
	newProtected := b.Get([]byte("p/"+newIdentity)) != nil
	updates := [][]byte{}
	cur := b.Cursor()
	for k, v := cur.Seek([]byte("a/")); k != nil && strings.HasPrefix(string(k), "a/"); k, v = cur.Next() {
		if string(v) == oldIdentity {
			updates = append(updates, append([]byte(nil), k...))
		}
	}
	for _, k := range updates {
		if err := b.Put(k, []byte(newIdentity)); err != nil {
			return err
		}
	}
	if oldIdentity != newIdentity {
		if oldProtected || newProtected {
			if err := b.Put([]byte("p/"+newIdentity), []byte{1}); err != nil {
				return err
			}
		}
		if err := b.Delete([]byte("p/" + oldIdentity)); err != nil {
			return err
		}
	}
	if err := b.Put(aliasKey(gid, token), []byte(newIdentity)); err != nil {
		return err
	}
	return b.Put(aliasKey(activeGID, activeToken), []byte(newIdentity))
}
func prefer(a, b []byte) bool {
	var x, y stored
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return string(a) < string(b)
	}
	if x.Doc.Modified.Equal(y.Doc.Modified) {
		return string(a) < string(b)
	}
	return x.Doc.Modified.After(y.Doc.Modified)
}
func (s *Store) Get(gid int, token string) ([]Document, error) {
	token = strings.ToLower(token)
	var out []Document
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		g, t := readAlias(b, gid, token)
		key := "r/" + identity(g, t)
		v := b.Get([]byte(key))
		if v == nil {
			return nil
		}
		var x stored
		if e := json.Unmarshal(v, &x); e != nil {
			return e
		}
		x.Doc.References = filterRefs(x.Doc.References, gid, token, x.GID, x.Token)
		out = append(out, x.Doc)
		return nil
	})
	return out, err
}
func filterRefs(refs []string, requestGID int, requestToken string, sourceGID int, sourceToken string) []string {
	if requestGID == sourceGID && requestToken == sourceToken {
		return refs
	}
	return []string{}
}
func (s *Store) Put(gid int, token string, d Document) (int, Document, error) {
	token = strings.ToLower(token)
	status := 201
	result := Document{}
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		g, t := readAlias(b, gid, token)
		archived := groupArchived(b, g, t)
		key := recKey(g, t)
		var old stored
		hadOld := false
		if v := b.Get(key); v != nil {
			hadOld = true
			if e := json.Unmarshal(v, &old); e != nil {
				return e
			}
			if d.Modified.Before(old.Doc.Modified) {
				return ErrConflict
			}
			if d.Modified.Equal(old.Doc.Modified) {
				a, _ := json.Marshal(d)
				c, _ := json.Marshal(old.Doc)
				if string(a) != string(c) {
					return ErrConflict
				}
				status = 200
				result = old.Doc
				result.References = filterRefs(result.References, gid, token, old.GID, old.Token)
				return nil
			}
			status = 200
		} else if !archived {
			if err := makeRoom(b, 1); err != nil {
				return err
			}
		}
		x := stored{GID: gid, Token: token, Doc: d, Written: time.Now().UnixNano()}
		raw, e := json.Marshal(x)
		if e != nil {
			return e
		}
		if err := b.Put(key, raw); err != nil {
			return err
		}
		if hadOld {
			if err := b.Delete(lruKey(old.Written, identity(g, t))); err != nil {
				return err
			}
		}
		if err := b.Put(lruKey(x.Written, identity(g, t)), []byte{}); err != nil {
			return err
		}
		result = d
		return nil
	})
	return status, result, err
}
func makeRoom(b *bolt.Bucket, additional int) error {
	protected := map[string]bool{}
	_ = b.ForEach(func(k, v []byte) error {
		if strings.HasPrefix(string(k), "p/") {
			protected[strings.TrimPrefix(string(k), "p/")] = true
		}
		return nil
	})
	count := 0
	c := b.Cursor()
	for k, _ := c.Seek([]byte("u/")); k != nil && strings.HasPrefix(string(k), "u/"); k, _ = c.Next() {
		parts := strings.SplitN(string(k)[2:], "/", 2)
		if len(parts) == 2 && !protected[parts[1]] {
			count++
		}
	}
	if count+additional <= MaxWorks {
		return nil
	}
	need := count + additional - MaxWorks
	victims := make([][]byte, 0, need)
	cur := b.Cursor()
	for k, _ := cur.Seek([]byte("u/")); k != nil && strings.HasPrefix(string(k), "u/") && len(victims) < need; k, _ = cur.Next() {
		parts := strings.SplitN(string(k)[2:], "/", 2)
		if len(parts) == 2 && !protected[parts[1]] {
			victims = append(victims, append([]byte(nil), k...))
		}
	}
	if len(victims) < need {
		return errors.New("progression storage capacity reached")
	}
	for _, victim := range victims {
		parts := strings.SplitN(string(victim)[2:], "/", 2)
		id := parts[1]
		if err := b.Delete(victim); err != nil {
			return err
		}
		if err := b.Delete([]byte("r/" + id)); err != nil {
			return err
		}
		aliases := [][]byte{}
		cur := b.Cursor()
		for k, v := cur.Seek([]byte("a/")); k != nil && strings.HasPrefix(string(k), "a/"); k, v = cur.Next() {
			if string(v) == id || string(k) == "a/"+id {
				aliases = append(aliases, append([]byte(nil), k...))
			}
		}
		for _, k := range aliases {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
	}
	return nil
}
func groupArchived(b *bolt.Bucket, gid int, token string) bool {
	return b.Get([]byte("p/"+identity(gid, token))) != nil
}

type Identity struct {
	GID   int
	Token string
}

type Group struct {
	Active     Identity
	Identities []Identity
	Available  []Identity
}

func (s *Store) ReconcileArchives(groups []Group) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		for _, g := range groups {
			if g.Active.GID <= 0 || g.Active.Token == "" {
				continue
			}
			for _, id := range g.Identities {
				if err := observeBucket(b, id.GID, id.Token, g.Active.GID, g.Active.Token); err != nil {
					return err
				}
			}
		}
		keys := [][]byte{}
		c := b.Cursor()
		for k, _ := c.Seek([]byte("z/")); k != nil && strings.HasPrefix(string(k), "z/"); k, _ = c.Next() {
			keys = append(keys, append([]byte(nil), k...))
		}
		for _, k := range keys {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		pkeys := [][]byte{}
		c = b.Cursor()
		for k, _ := c.Seek([]byte("p/")); k != nil && strings.HasPrefix(string(k), "p/"); k, _ = c.Next() {
			pkeys = append(pkeys, append([]byte(nil), k...))
		}
		for _, k := range pkeys {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		protected := map[string]bool{}
		for _, g := range groups {
			if len(g.Available) > 0 {
				for _, id := range g.Identities {
					if id.GID > 0 && id.Token != "" {
						protected[identity(id.GID, id.Token)] = true
					}
				}
			}
			for _, id := range g.Available {
				if id.GID > 0 && id.Token != "" {
					canonicalG, canonicalT := readAlias(b, id.GID, id.Token)
					canonical := identity(canonicalG, canonicalT)
					if err := b.Put([]byte("z/"+identity(id.GID, id.Token)), []byte{1}); err != nil {
						return err
					}
					protected[canonical] = true
				}
			}
		}
		for id := range protected {
			if err := b.Put([]byte("p/"+id), []byte{1}); err != nil {
				return err
			}
		}
		if err := trimAliases(b); err != nil {
			return err
		}
		return makeRoom(b, 0)
	})
}

func trimAliases(b *bolt.Bucket) error {
	protected := map[string]bool{}
	_ = b.ForEach(func(k, v []byte) error {
		if strings.HasPrefix(string(k), "p/") {
			protected[strings.TrimPrefix(string(k), "p/")] = true
		}
		return nil
	})
	aliases := [][]byte{}
	c := b.Cursor()
	for k, v := c.Seek([]byte("a/")); k != nil && strings.HasPrefix(string(k), "a/"); k, v = c.Next() {
		target := string(v)
		if b.Get([]byte("r/"+target)) != nil {
			continue
		}
		aliases = append(aliases, append([]byte(nil), k...))
	}
	for len(aliases) > 20000 {
		if err := b.Delete(aliases[0]); err != nil {
			return err
		}
		aliases = aliases[1:]
	}
	return nil
}

func (s *Store) Prune() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return makeRoom(tx.Bucket([]byte(bucket)), 0)
	})
}
func (s *Store) Count() (int, error) {
	works := map[string]bool{}
	e := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucket)).ForEach(func(k, v []byte) error {
			if strings.HasPrefix(string(k), "r/") {
				works[strings.TrimPrefix(string(k), "r/")] = true
			}
			return nil
		})
	})
	return len(works), e
}
