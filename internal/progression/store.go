package progression

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
	"pandaopds/internal/eh"
	"pandaopds/internal/storage"
)

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
type Snapshot struct {
	Title       string          `json:"title"`
	TitleJPN    string          `json:"title_jpn,omitempty"`
	Category    string          `json:"category,omitempty"`
	Tags        []eh.GalleryTag `json:"tags,omitempty"`
	CoverURL    string          `json:"cover_url,omitempty"`
	PageCount   *int            `json:"page_count,omitempty"`
	Rating      float64         `json:"rating,omitempty"`
	PublishTime string          `json:"publish_time,omitempty"`
	Language    string          `json:"language,omitempty"`
	Expunged    bool            `json:"expunged,omitempty"`
}
type stored struct {
	GID       int             `json:"gid"`
	Token     string          `json:"token"`
	Doc       Document        `json:"doc"`
	Written   int64           `json:"written"`
	Snapshot  *Snapshot       `json:"snapshot,omitempty"`
	LegacyRaw json.RawMessage `json:"-"`
}
type Store struct {
	db      *sql.DB
	owner   *storage.DB
	writeMu sync.Mutex
}

// Open is retained for callers/tests that own a standalone progression store.
// The server instead injects its process-wide SQLite database with OpenDB.
func Open(stateDir string) (*Store, error) {
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return nil, err
	}
	db, err := storage.Open(filepath.Join(stateDir, "pandaopds.db"))
	if err != nil {
		return nil, err
	}
	s := &Store{db: db.DB, owner: db}
	if err = s.ImportBolt(filepath.Join(stateDir, "opds-progression.db")); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func OpenDB(db *storage.DB, legacyPath string) (*Store, error) {
	s := &Store{db: db.DB}
	if legacyPath != "" {
		if err := s.ImportBolt(legacyPath); err != nil {
			return nil, err
		}
	}
	return s, nil
}
func (s *Store) Close() error {
	if s.owner != nil {
		return s.owner.Close()
	}
	return nil
}
func identity(gid int, token string) string { return strconv.Itoa(gid) + "/" + strings.ToLower(token) }
func (s *Store) canonical(gid int, token string) (int, string, error) {
	var target string
	err := s.db.QueryRow(`SELECT target_id FROM progression_aliases WHERE alias=? AND target_type='eh'`, identity(gid, token)).Scan(&target)
	if err == sql.ErrNoRows {
		return gid, strings.ToLower(token), nil
	}
	if err != nil {
		return 0, "", err
	}
	p := strings.SplitN(target, "/", 2)
	if len(p) != 2 {
		return gid, strings.ToLower(token), nil
	}
	g, e := strconv.Atoi(p[0])
	if e != nil {
		return gid, strings.ToLower(token), nil
	}
	return g, p[1], nil
}
func (s *Store) Observe(gid int, token string, activeGID int, activeToken string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if gid < 1 || token == "" || activeGID < 1 || activeToken == "" {
		return errors.New("invalid gallery identity")
	}
	token = strings.ToLower(token)
	activeToken = strings.ToLower(activeToken)
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	og, ot, e := s.canonicalTx(tx, gid, token)
	if e != nil {
		return e
	}
	ng, nt := og, ot
	if gid != activeGID || token != activeToken {
		ng, nt, e = s.canonicalTx(tx, activeGID, activeToken)
		if e != nil {
			return e
		}
	}
	oldID, newID := identity(og, ot), identity(ng, nt)
	if oldID != newID {
		if e = mergeRecords(tx, oldID, newID); e != nil {
			return e
		}
		_, e = tx.Exec(`UPDATE progression_aliases SET target_id=? WHERE target_type='eh' AND target_id=?`, newID, oldID)
		if e != nil {
			return e
		}
		var n int
		_ = tx.QueryRow(`SELECT COUNT(*) FROM progression_protected WHERE target_type='eh' AND target_id IN (?,?)`, oldID, newID).Scan(&n)
		if n > 0 {
			_, e = tx.Exec(`INSERT OR IGNORE INTO progression_protected VALUES('eh',?)`, newID)
			if e != nil {
				return e
			}
		}
		_, e = tx.Exec(`DELETE FROM progression_protected WHERE target_type='eh' AND target_id=?`, oldID)
		if e != nil {
			return e
		}
	}
	for _, x := range []struct {
		g int
		t string
	}{{gid, token}, {activeGID, activeToken}} {
		_, e = tx.Exec(`INSERT INTO progression_aliases(alias,target_id,target_type) VALUES(?,?,'eh') ON CONFLICT(alias) DO UPDATE SET target_id=excluded.target_id WHERE progression_aliases.target_id<>excluded.target_id`, identity(x.g, x.t), newID)
		if e != nil {
			return e
		}
	}
	if e = trimAliases(tx); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) canonicalTx(tx *sql.Tx, gid int, token string) (int, string, error) {
	var target string
	e := tx.QueryRow(`SELECT target_id FROM progression_aliases WHERE alias=? AND target_type='eh'`, identity(gid, token)).Scan(&target)
	if e == sql.ErrNoRows {
		return gid, strings.ToLower(token), nil
	}
	if e != nil {
		return 0, "", e
	}
	p := strings.SplitN(target, "/", 2)
	if len(p) != 2 {
		return gid, strings.ToLower(token), nil
	}
	g, e := strconv.Atoi(p[0])
	if e != nil {
		return gid, strings.ToLower(token), nil
	}
	return g, p[1], nil
}
func (s *Store) Get(gid int, token string) ([]Document, error) {
	g, t, e := s.canonical(gid, token)
	if e != nil {
		return nil, e
	}
	var d Document
	var mod, dev, refs, storedToken string
	var storedGID int
	var title sql.NullString
	e = s.db.QueryRow(`SELECT gid,token,modified,device_json,progression,title,references_json FROM progressions WHERE target_type='eh' AND target_id=?`, identity(g, t)).Scan(&storedGID, &storedToken, &mod, &dev, &d.Progression, &title, &refs)
	if e == sql.ErrNoRows {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	d.Modified, _ = time.Parse(time.RFC3339Nano, mod)
	if title.Valid {
		d.Title = &title.String
	}
	_ = json.Unmarshal([]byte(dev), &d.Device)
	_ = json.Unmarshal([]byte(refs), &d.References)
	if storedGID != gid || storedToken != strings.ToLower(token) {
		d.References = []string{}
	}
	return []Document{d}, nil
}
func (s *Store) Put(gid int, token string, d Document) (int, Document, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, e := s.db.Begin()
	if e != nil {
		return 0, Document{}, e
	}
	defer tx.Rollback()
	g, t, e := s.canonicalTx(tx, gid, token)
	if e != nil {
		return 0, Document{}, e
	}
	id := identity(g, t)
	var old Document
	var oldModified string
	var oldGID int
	var oldToken string
	var dev, refs string
	var snap sql.NullString
	var written int64
	e = tx.QueryRow(`SELECT gid,token,modified,device_json,progression,title,references_json,written,snapshot_json FROM progressions WHERE target_type='eh' AND target_id=?`, id).Scan(&oldGID, &oldToken, &oldModified, &dev, &old.Progression, &old.Title, &refs, &written, &snap)
	had := e == nil
	if e != nil && e != sql.ErrNoRows {
		return 0, Document{}, e
	}
	if had {
		old.Modified, _ = time.Parse(time.RFC3339Nano, oldModified)
		_ = json.Unmarshal([]byte(dev), &old.Device)
		_ = json.Unmarshal([]byte(refs), &old.References)
		if d.Modified.Before(old.Modified) {
			return 0, Document{}, ErrConflict
		}
		if d.Modified.Equal(old.Modified) {
			a, _ := json.Marshal(d)
			b, _ := json.Marshal(old)
			if string(a) != string(b) {
				return 0, Document{}, ErrConflict
			}
			if oldGID != gid || oldToken != strings.ToLower(token) {
				old.References = []string{}
			}
			return 200, old, nil
		}
	}
	status := 201
	if had {
		status = 200
	}
	var protected int
	_ = tx.QueryRow(`SELECT COUNT(*) FROM progression_protected WHERE target_type='eh' AND target_id=?`, id).Scan(&protected)
	if !had && protected == 0 {
		if e = makeRoom(tx, 1); e != nil {
			return 0, Document{}, e
		}
	}
	devBytes, _ := json.Marshal(d.Device)
	refsBytes, _ := json.Marshal(d.References)
	if len(refsBytes) == 0 {
		refsBytes = []byte("[]")
	}
	var snapValue any
	if snap.Valid {
		snapValue = snap.String
	}
	_, e = tx.Exec(`INSERT INTO progressions(target_type,target_id,gid,token,modified,device_json,progression,title,references_json,written,snapshot_json) VALUES('eh',?,?,?,?,?,?,?,?,?,?) ON CONFLICT(target_type,target_id) DO UPDATE SET gid=excluded.gid,token=excluded.token,modified=excluded.modified,device_json=excluded.device_json,progression=excluded.progression,title=excluded.title,references_json=excluded.references_json,written=excluded.written`, id, gid, strings.ToLower(token), d.Modified.UTC().Format(time.RFC3339Nano), string(devBytes), d.Progression, d.Title, string(refsBytes), time.Now().UnixNano(), snapValue)
	if e != nil {
		return 0, Document{}, e
	}
	return status, d, tx.Commit()
}
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

type HistoryItem struct {
	GID      int       `json:"gid"`
	Token    string    `json:"token"`
	Written  int64     `json:"written"`
	Doc      Document  `json:"doc"`
	Snapshot *Snapshot `json:"snapshot,omitempty"`
}

type HistoryQuery struct {
	Offset, Limit           int
	ReadingStatus, Category string
}

// ListHistory applies the production filters in SQL so pagination does not
// decode or scan every progression record in Go.
func (s *Store) ListHistoryQuery(q HistoryQuery) ([]HistoryItem, int, error) {
	if q.Offset < 0 {
		q.Offset = 0
	}
	if q.Limit <= 0 {
		q.Limit = 20
	}
	where := ` WHERE target_type='eh'`
	args := []any{}
	switch strings.ToLower(q.ReadingStatus) {
	case "unread":
		where += ` AND progression<0.01`
	case "reading":
		where += ` AND progression>=0.01 AND progression<0.95`
	case "completed":
		where += ` AND progression>=0.95`
	}
	if q.Category != "" {
		where += ` AND lower(CASE WHEN snapshot_category='' THEN 'Misc' ELSE snapshot_category END)=lower(?)`
		args = append(args, q.Category)
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM progressions`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, q.Limit, q.Offset)
	rows, err := s.db.Query(`SELECT gid,token,written,modified,device_json,progression,title,references_json,snapshot_json FROM progressions`+where+` ORDER BY written DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []HistoryItem{}
	for rows.Next() {
		var x HistoryItem
		var mod, dev, refs string
		var title, snap sql.NullString
		if err = rows.Scan(&x.GID, &x.Token, &x.Written, &mod, &dev, &x.Doc.Progression, &title, &refs, &snap); err != nil {
			return nil, 0, err
		}
		x.Doc.Modified, _ = time.Parse(time.RFC3339Nano, mod)
		_ = json.Unmarshal([]byte(dev), &x.Doc.Device)
		_ = json.Unmarshal([]byte(refs), &x.Doc.References)
		if title.Valid {
			x.Doc.Title = &title.String
		}
		if snap.Valid {
			var v Snapshot
			if json.Unmarshal([]byte(snap.String), &v) == nil {
				x.Snapshot = &v
			}
		}
		out = append(out, x)
	}
	return out, total, rows.Err()
}

func MatchReadingStatus(has bool, p float64, f string) bool {
	switch strings.ToLower(f) {
	case "unread":
		return !has || p < .01
	case "reading":
		return has && p >= .01 && p < .95
	case "completed":
		return has && p >= .95
	default:
		return true
	}
}
func (s *Store) ListHistory(offset, limit int) ([]HistoryItem, int, error) {
	return s.ListHistoryFiltered(offset, limit, nil)
}
func (s *Store) ListHistoryFiltered(offset, limit int, predicate func(HistoryItem) bool) ([]HistoryItem, int, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 20
	}
	rows, e := s.db.Query(`SELECT gid,token,written,modified,device_json,progression,title,references_json,snapshot_json FROM progressions WHERE target_type='eh' ORDER BY written DESC`)
	if e != nil {
		return nil, 0, e
	}
	defer rows.Close()
	items := []HistoryItem{}
	total := 0
	for rows.Next() {
		var x HistoryItem
		var mod, dev, refs string
		var title sql.NullString
		var snap sql.NullString
		if e = rows.Scan(&x.GID, &x.Token, &x.Written, &mod, &dev, &x.Doc.Progression, &title, &refs, &snap); e != nil {
			return nil, 0, e
		}
		x.Doc.Modified, _ = time.Parse(time.RFC3339Nano, mod)
		_ = json.Unmarshal([]byte(dev), &x.Doc.Device)
		_ = json.Unmarshal([]byte(refs), &x.Doc.References)
		if title.Valid {
			x.Doc.Title = &title.String
		}
		if snap.Valid {
			var s Snapshot
			if json.Unmarshal([]byte(snap.String), &s) == nil {
				x.Snapshot = &s
			}
		}
		if predicate != nil && !predicate(x) {
			continue
		}
		if total >= offset && len(items) < limit {
			items = append(items, x)
		}
		total++
	}
	return items, total, rows.Err()
}
func (s *Store) GetProgressions() (map[string]float64, error) {
	m := map[string]float64{}
	rows, e := s.db.Query(`SELECT target_id,progression FROM progressions WHERE target_type='eh'`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var p float64
		if e = rows.Scan(&k, &p); e != nil {
			return nil, e
		}
		m[k] = p
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return nil, e
	}
	if e = rows.Close(); e != nil {
		return nil, e
	}
	rows, e = s.db.Query(`SELECT alias,target_id FROM progression_aliases WHERE target_type='eh'`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var a, t string
		if e = rows.Scan(&a, &t); e != nil {
			return nil, e
		}
		if p, ok := m[t]; ok {
			m[a] = p
		}
	}
	e = rows.Err()
	return m, errors.Join(e, rows.Close())
}
func makeRoom(tx *sql.Tx, additional int) error {
	var n int
	if e := tx.QueryRow(`SELECT COUNT(*) FROM progressions p WHERE p.target_type='eh' AND NOT EXISTS(SELECT 1 FROM progression_protected x WHERE x.target_type=p.target_type AND x.target_id=p.target_id)`).Scan(&n); e != nil {
		return e
	}
	need := n + additional - MaxWorks
	if need <= 0 {
		return nil
	}
	rows, e := tx.Query(`SELECT target_id FROM progressions p WHERE p.target_type='eh' AND NOT EXISTS(SELECT 1 FROM progression_protected x WHERE x.target_type=p.target_type AND x.target_id=p.target_id) ORDER BY written LIMIT ?`, need)
	if e != nil {
		return e
	}
	keys := []string{}
	for rows.Next() {
		var k string
		if e = rows.Scan(&k); e != nil {
			rows.Close()
			return e
		}
		keys = append(keys, k)
	}
	rows.Close()
	if len(keys) < need {
		return errors.New("progression storage capacity reached")
	}
	for _, k := range keys {
		if _, e = tx.Exec(`DELETE FROM progressions WHERE target_type='eh' AND target_id=?`, k); e != nil {
			return e
		}
		if _, e = tx.Exec(`DELETE FROM progression_aliases WHERE target_type='eh' AND (target_id=? OR alias=?)`, k, k); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) ReconcileArchives(groups []Group) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, g := range groups {
		if g.Active.GID <= 0 || g.Active.Token == "" {
			continue
		}
		for _, id := range g.Identities {
			if e = s.observeTx(tx, id.GID, id.Token, g.Active.GID, g.Active.Token); e != nil {
				return e
			}
		}
	}
	_, e = tx.Exec(`DELETE FROM progression_protected WHERE target_type='eh'`)
	if e != nil {
		return e
	}
	for _, g := range groups {
		if len(g.Available) > 0 {
			for _, id := range g.Identities {
				_, e = tx.Exec(`INSERT OR IGNORE INTO progression_protected VALUES('eh',?)`, identity(id.GID, id.Token))
				if e != nil {
					return e
				}
			}
		}
		for _, id := range g.Available {
			_, e = tx.Exec(`INSERT OR IGNORE INTO progression_aliases(alias,target_id,target_type) VALUES(?,?,'eh')`, identity(id.GID, id.Token), identity(g.Active.GID, g.Active.Token))
			if e != nil {
				return e
			}
			_, e = tx.Exec(`INSERT OR IGNORE INTO progression_protected VALUES('eh',?)`, identity(g.Active.GID, g.Active.Token))
			if e != nil {
				return e
			}
		}
	}
	if e = trimAliases(tx); e != nil {
		return e
	}
	if e = makeRoom(tx, 0); e != nil {
		return e
	}
	return tx.Commit()
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

func (s *Store) observeTx(tx *sql.Tx, gid int, token string, ag int, at string) error {
	og, ot, e := s.canonicalTx(tx, gid, token)
	if e != nil {
		return e
	}
	ng, nt, e := s.canonicalTx(tx, ag, at)
	if e != nil {
		return e
	}
	old, new := identity(og, ot), identity(ng, nt)
	if old != new {
		if e = mergeRecords(tx, old, new); e != nil {
			return e
		}
		_, e = tx.Exec(`UPDATE progression_aliases SET target_id=? WHERE target_type='eh' AND target_id=?`, new, old)
		if e != nil {
			return e
		}
	}
	for _, x := range []struct {
		g int
		t string
	}{{gid, token}, {ag, at}} {
		_, e = tx.Exec(`INSERT INTO progression_aliases(alias,target_id,target_type) VALUES(?,?,'eh') ON CONFLICT(alias) DO UPDATE SET target_id=excluded.target_id WHERE progression_aliases.target_id<>excluded.target_id`, identity(x.g, x.t), new)
		if e != nil {
			return e
		}
	}
	return nil
}

func trimAliases(tx *sql.Tx) error {
	_, err := tx.Exec(`DELETE FROM progression_aliases WHERE alias IN (SELECT a.alias FROM progression_aliases a WHERE a.target_type='eh' AND NOT EXISTS(SELECT 1 FROM progressions p WHERE p.target_type=a.target_type AND p.target_id=a.target_id) ORDER BY a.rowid LIMIT MAX(0,(SELECT COUNT(*) FROM progression_aliases a WHERE a.target_type='eh' AND NOT EXISTS(SELECT 1 FROM progressions p WHERE p.target_type=a.target_type AND p.target_id=a.target_id))-20000))`)
	return err
}

func loadStored(tx *sql.Tx, id string) (*stored, error) {
	var x stored
	var modified, device, refs string
	var title, snapshot sql.NullString
	var legacyRaw []byte
	err := tx.QueryRow(`SELECT gid,token,modified,device_json,progression,title,references_json,written,snapshot_json,legacy_raw_json FROM progressions WHERE target_type='eh' AND target_id=?`, id).Scan(&x.GID, &x.Token, &modified, &device, &x.Doc.Progression, &title, &refs, &x.Written, &snapshot, &legacyRaw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	x.Doc.Modified, _ = time.Parse(time.RFC3339Nano, modified)
	x.LegacyRaw = legacyRaw
	_ = json.Unmarshal([]byte(device), &x.Doc.Device)
	_ = json.Unmarshal([]byte(refs), &x.Doc.References)
	if title.Valid {
		x.Doc.Title = &title.String
	}
	if snapshot.Valid {
		var snap Snapshot
		if json.Unmarshal([]byte(snapshot.String), &snap) == nil {
			x.Snapshot = &snap
		}
	}
	return &x, nil
}
func mergeRecords(tx *sql.Tx, oldID, newID string) error {
	old, e := loadStored(tx, oldID)
	if e != nil {
		return e
	}
	dest, e := loadStored(tx, newID)
	if e != nil {
		return e
	}
	chosen := dest
	if old != nil && (dest == nil || old.Doc.Modified.After(dest.Doc.Modified)) {
		chosen = old
	} else if old != nil && dest != nil && old.Doc.Modified.Equal(dest.Doc.Modified) {
		a, b := old.LegacyRaw, dest.LegacyRaw
		if len(a) == 0 {
			a, _ = json.Marshal(old)
		}
		if len(b) == 0 {
			b, _ = json.Marshal(dest)
		}
		if string(a) < string(b) {
			chosen = old
		}
	}
	if _, e = tx.Exec(`DELETE FROM progressions WHERE target_type='eh' AND target_id IN (?,?)`, oldID, newID); e != nil {
		return e
	}
	if chosen != nil {
		dev, _ := json.Marshal(chosen.Doc.Device)
		refs, _ := json.Marshal(chosen.Doc.References)
		var title, snapshot any
		if chosen.Doc.Title != nil {
			title = *chosen.Doc.Title
		}
		if chosen.Snapshot != nil {
			raw, _ := json.Marshal(chosen.Snapshot)
			snapshot = string(raw)
		}
		if _, e = tx.Exec(`INSERT INTO progressions(target_type,target_id,gid,token,modified,device_json,progression,title,references_json,written,snapshot_json,snapshot_category,legacy_raw_json) VALUES('eh',?,?,?,?,?,?,?,?,?,?,?,?)`, newID, chosen.GID, strings.ToLower(chosen.Token), chosen.Doc.Modified.UTC().Format(time.RFC3339Nano), string(dev), chosen.Doc.Progression, title, string(refs), chosen.Written, snapshot, categoryOf(chosen.Snapshot), chosen.LegacyRaw); e != nil {
			return e
		}
	}
	return nil
}
func categoryOf(s *Snapshot) string {
	if s == nil {
		return ""
	}
	return s.Category
}
func (s *Store) SetSnapshot(gid int, token string, snap Snapshot) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	b, e := json.Marshal(snap)
	if e != nil {
		return e
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	g, t, e := s.canonicalTx(tx, gid, token)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(`UPDATE progressions SET snapshot_json=?,snapshot_category=? WHERE target_type='eh' AND target_id=?`, string(b), snap.Category, identity(g, t)); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Prune() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = makeRoom(tx, 0); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Count() (int, error) {
	var n int
	e := s.db.QueryRow(`SELECT COUNT(*) FROM progressions`).Scan(&n)
	return n, e
}

// ImportBolt copies every progression bucket record and its alias/protection
// state exactly once. The old file remains untouched for rollback.
func (s *Store) ImportBolt(path string) error {
	var n int64
	if e := s.db.QueryRow(`SELECT COUNT(*) FROM legacy_imports WHERE name='bbolt_progression'`).Scan(&n); e != nil {
		return e
	}
	if n > 0 {
		return nil
	}
	if _, e := os.Stat(path); os.IsNotExist(e) {
		return nil
	}
	b, e := bolt.Open(path, 0600, &bolt.Options{ReadOnly: true, Timeout: time.Second})
	if e != nil {
		return e
	}
	defer b.Close()
	sqltx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer sqltx.Rollback()
	var records, aliases, protected int
	err := b.View(func(btx *bolt.Tx) error {
		bucket := btx.Bucket([]byte("progression-v1"))
		if bucket == nil {
			return nil
		}
		return bucket.ForEach(func(k, v []byte) error {
			key := string(k)
			if strings.HasPrefix(key, "r/") {
				var x stored
				if e := json.Unmarshal(v, &x); e != nil {
					return e
				}
				id := identity(x.GID, x.Token)
				dev, _ := json.Marshal(x.Doc.Device)
				refs, _ := json.Marshal(x.Doc.References)
				var snap any
				if x.Snapshot != nil {
					z, _ := json.Marshal(x.Snapshot)
					snap = string(z)
				}
				_, e := sqltx.Exec(`INSERT OR IGNORE INTO progressions(target_type,target_id,gid,token,modified,device_json,progression,title,references_json,written,snapshot_json,snapshot_category,legacy_raw_json) VALUES('eh',?,?,?,?,?,?,?,?,?,?,?,?)`, id, x.GID, strings.ToLower(x.Token), x.Doc.Modified.UTC().Format(time.RFC3339Nano), string(dev), x.Doc.Progression, x.Doc.Title, string(refs), x.Written, snap, categoryOf(x.Snapshot), []byte(v))
				records++
				return e
			}
			if strings.HasPrefix(key, "a/") {
				alias := strings.TrimPrefix(key, "a/")
				_, e := sqltx.Exec(`INSERT OR IGNORE INTO progression_aliases(alias,target_id,target_type) VALUES(?,?,'eh')`, alias, string(v))
				aliases++
				return e
			}
			if strings.HasPrefix(key, "p/") {
				_, e := sqltx.Exec(`INSERT OR IGNORE INTO progression_protected VALUES('eh',?)`, strings.TrimPrefix(key, "p/"))
				protected++
				return e
			}
			return nil
		})
	})
	if err != nil {
		return err
	}
	if _, e = sqltx.Exec(`INSERT INTO legacy_imports(name,completed_at,details_json) VALUES('bbolt_progression',?,?)`, time.Now().UTC().Format(time.RFC3339Nano), fmt.Sprintf(`{"source":%q,"records":%d,"aliases":%d,"protected":%d}`, path, records, aliases, protected)); e != nil {
		return e
	}
	return sqltx.Commit()
}
