package favorites

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

type State struct {
	path string
	mu   sync.RWMutex
	data map[string]any
	db   *sql.DB
}

// NewDBState imports the legacy favorites snapshot once, then persists every
// state update in SQLite. The legacy file is read only and retained for rollback.
func NewDBState(db *sql.DB, path string) (*State, error) {
	if db == nil {
		return nil, fmt.Errorf("favorites database is nil")
	}
	s := NewState(path)
	s.db = db
	var raw string
	err := db.QueryRow(`SELECT value_json FROM app_state WHERE state_key='favorites_sync'`).Scan(&raw)
	if err == nil {
		if e := json.Unmarshal([]byte(raw), &s.data); e != nil {
			return nil, e
		}
		return s, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	b, e := json.Marshal(s.data)
	if e != nil {
		return nil, e
	}
	tx, e := db.Begin()
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`INSERT INTO app_state(state_key,value_json,updated_at) VALUES('favorites_sync',?,?)`, string(b), time.Now().UTC().Format(time.RFC3339Nano)); e != nil {
		return nil, e
	}
	if _, e = tx.Exec(`INSERT OR IGNORE INTO legacy_imports(name,completed_at,details_json) VALUES('favorites_sync',?,?)`, time.Now().UTC().Format(time.RFC3339Nano), fmt.Sprintf(`{"source":%q}`, path)); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return s, nil
}

func NewState(path string) *State {
	s := &State{path: path, data: map[string]any{}}
	s.load()
	return s
}
func (s *State) load() {
	b, e := os.ReadFile(s.path)
	if e == nil {
		_ = json.Unmarshal(b, &s.data)
	}
	if s.data == nil {
		s.data = map[string]any{}
	}
	defaults := map[string]any{"known": []string{}, "archived": []string{}, "errors": map[string]string{}, "favcats": map[string]int{}, "favcat_map": map[string]string{}, "baseline": false, "catalog_backfilled": false, "last_run": float64(0), "last_ok": true, "scanned_pages": 0}
	for k, v := range defaults {
		if _, ok := s.data[k]; !ok {
			s.data[k] = v
		}
	}
}
func (s *State) saveLocked() error {
	if s.db != nil {
		b, e := json.Marshal(s.data)
		if e != nil {
			return e
		}
		_, e = s.db.Exec(`INSERT INTO app_state(state_key,value_json,updated_at) VALUES('favorites_sync',?,?) ON CONFLICT(state_key) DO UPDATE SET value_json=excluded.value_json,updated_at=excluded.updated_at`, string(b), time.Now().UTC().Format(time.RFC3339Nano))
		return e
	}
	if e := os.MkdirAll(filepath.Dir(s.path), 0o755); e != nil {
		return e
	}
	b, e := json.MarshalIndent(s.data, "", "  ")
	if e != nil {
		return e
	}
	tmp := filepath.Join(filepath.Dir(s.path), "favorites_sync.json.tmp")
	if e = os.WriteFile(tmp, b, 0o600); e != nil {
		return e
	}
	return os.Rename(tmp, s.path)
}
func (s *State) stringSet(key string) map[string]bool {
	raw, _ := s.data[key].([]any)
	out := map[string]bool{}
	for _, v := range raw {
		if x, ok := v.(string); ok {
			out[x] = true
		}
	}
	if typed, ok := s.data[key].([]string); ok {
		for _, x := range typed {
			out[x] = true
		}
	}
	return out
}
func (s *State) Known() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stringSet("known")
}
func (s *State) Archived() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stringSet("archived")
}
func (s *State) Baseline() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, _ := s.data["baseline"].(bool)
	return v
}
func (s *State) CatalogBackfilled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, _ := s.data["catalog_backfilled"].(bool)
	return v
}
func (s *State) MarkCatalogBackfilled(v bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data["catalog_backfilled"] = v
	return s.saveLocked()
}
func (s *State) Errors() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]string{}
	if raw, ok := s.data["errors"].(map[string]any); ok {
		for k, v := range raw {
			out[k] = toString(v)
		}
	}
	if raw, ok := s.data["errors"].(map[string]string); ok {
		for k, v := range raw {
			out[k] = v
		}
	}
	return out
}
func (s *State) Favcats() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]int{}
	if raw, ok := s.data["favcats"].(map[string]any); ok {
		for k, v := range raw {
			out[k] = toInt(v)
		}
	}
	if raw, ok := s.data["favcats"].(map[string]int); ok {
		for k, v := range raw {
			out[k] = v
		}
	}
	return out
}
func (s *State) FavcatMap() map[int]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[int]string{}
	switch raw := s.data["favcat_map"].(type) {
	case map[string]any:
		for k, v := range raw {
			if id, e := strconv.Atoi(k); e == nil {
				out[id] = toString(v)
			}
		}
	case map[string]string:
		for k, v := range raw {
			if id, e := strconv.Atoi(k); e == nil {
				out[id] = v
			}
		}
	}
	return out
}
func (s *State) Summary() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{"known_count": len(s.stringSet("known")), "archived_count": len(s.stringSet("archived")), "error_count": len(s.ErrorsUnsafe()), "baseline": s.data["baseline"], "last_run": s.data["last_run"], "last_ok": s.data["last_ok"], "scanned_pages": s.data["scanned_pages"]}
}

// LegacyDiagnostics returns a detached snapshot of import-only sync metadata.
func (s *State) LegacyDiagnostics() (baseline bool, lastRun float64, lastOK bool, pages int, errs map[string]string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	baseline, _ = s.data["baseline"].(bool)
	switch v := s.data["last_run"].(type) {
	case float64:
		lastRun = v
	case int:
		lastRun = float64(v)
	case int64:
		lastRun = float64(v)
	}
	lastOK, _ = s.data["last_ok"].(bool)
	pages = toInt(s.data["scanned_pages"])
	errs = s.ErrorsUnsafe()
	return
}
func (s *State) ErrorsUnsafe() map[string]string {
	out := map[string]string{}
	if raw, ok := s.data["errors"].(map[string]any); ok {
		for k, v := range raw {
			out[k] = toString(v)
		}
	}
	if raw, ok := s.data["errors"].(map[string]string); ok {
		for k, v := range raw {
			out[k] = v
		}
	}
	return out
}
func (s *State) Update(known, archived map[string]bool, errors map[string]string, favcats map[string]int, favcatMap map[int]string, baseline, lastOK *bool, pages *int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if known != nil {
		s.data["known"] = sortedKeys(known)
	}
	if archived != nil {
		s.data["archived"] = sortedKeys(archived)
	}
	if errors != nil {
		s.data["errors"] = errors
	}
	if favcats != nil {
		s.data["favcats"] = favcats
	}
	if favcatMap != nil {
		raw := map[string]string{}
		if old, ok := s.data["favcat_map"].(map[string]any); ok {
			for k, v := range old {
				raw[k] = toString(v)
			}
		}
		for k, v := range favcatMap {
			raw[strconv.Itoa(k)] = v
		}
		s.data["favcat_map"] = raw
	}
	if baseline != nil {
		s.data["baseline"] = *baseline
	}
	if lastOK != nil {
		s.data["last_ok"] = *lastOK
	}
	if pages != nil {
		s.data["scanned_pages"] = *pages
	}
	s.data["last_run"] = float64(time.Now().Unix())
	return s.saveLocked()
}
func (s *State) Discard(keys map[string]bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range []string{"known", "archived"} {
		set := s.stringSet(k)
		for x := range keys {
			delete(set, x)
		}
		s.data[k] = sortedKeys(set)
	}
	for _, k := range []string{"errors", "favcats"} {
		m, _ := s.data[k].(map[string]any)
		for x := range keys {
			delete(m, x)
		}
		s.data[k] = m
	}
	s.data["last_run"] = float64(time.Now().Unix())
	return s.saveLocked()
}

// ClearArchiveTracking removes keys from archived and errors sets while
// preserving known and favcats membership.
func (s *State) ClearArchiveTracking(keys map[string]bool) error {
	if len(keys) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	archived := s.stringSet("archived")
	for x := range keys {
		delete(archived, x)
	}
	s.data["archived"] = sortedKeys(archived)
	errs := s.ErrorsUnsafe()
	for x := range keys {
		delete(errs, x)
	}
	s.data["errors"] = errs
	return s.saveLocked()
}
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func toString(v any) string { s, _ := v.(string); return s }
func toInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case float64:
		return int(x)
	case int64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	}
	return 0
}
