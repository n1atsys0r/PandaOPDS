package archive

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pandaopds/internal/eh"
)

const (
	metaName     = "meta.json"
	zipName      = "archive.zip"
	partName     = "archive.part"
	metadataName = "metadata.json"
	coverName    = "cover.jpg"
)

type Store struct {
	Root    string
	mu      sync.RWMutex
	entries map[string]map[string]any
	names   map[string][]string
}

func NewStore(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	s := &Store{Root: root, entries: map[string]map[string]any{}, names: map[string][]string{}}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != metaName {
			return nil
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return nil
		}
		m := map[string]any{}
		if json.Unmarshal(b, &m) != nil {
			return nil
		}
		gid := toInt(m["gid"])
		tok, _ := m["token"].(string)
		if gid > 0 && tok != "" {
			s.entries[key(gid, tok)] = m
		}
		return nil
	})
	return s, nil
}
func key(gid int, token string) string { return strconv.Itoa(gid) + ":" + token }
func (s *Store) dir(gid int, token string) string {
	return filepath.Join(s.Root, strconv.Itoa(gid), token)
}
func (s *Store) Get(gid int, token string) map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.entries[key(gid, token)]
	if m == nil {
		return nil
	}
	out := cloneMap(m)
	if st, e := os.Stat(filepath.Join(s.dir(gid, token), zipName)); e == nil {
		out["archive_size"] = st.Size()
	} else {
		out["archive_size"] = int64(0)
	}
	if st, e := os.Stat(filepath.Join(s.dir(gid, token), partName)); e == nil {
		out["part_size"] = st.Size()
	} else {
		out["part_size"] = int64(0)
	}
	return out
}
func (s *Store) Upsert(gid int, token string, patch map[string]any) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(gid, token)
	m := cloneMap(s.entries[k])
	if m == nil {
		m = map[string]any{"created_at": float64(time.Now().Unix())}
	}
	for k, v := range patch {
		m[k] = v
	}
	m["gid"] = gid
	m["token"] = token
	m["updated_at"] = float64(time.Now().Unix())
	if err := os.MkdirAll(s.dir(gid, token), 0o755); err != nil {
		return nil, err
	}
	b, e := json.MarshalIndent(m, "", "  ")
	if e != nil {
		return nil, e
	}
	tmp := filepath.Join(s.dir(gid, token), metaName+".tmp")
	if e = os.WriteFile(tmp, b, 0o600); e != nil {
		return nil, e
	}
	if e = os.Rename(tmp, filepath.Join(s.dir(gid, token), metaName)); e != nil {
		return nil, e
	}
	s.entries[k] = m
	return cloneMap(m), nil
}
func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	b, _ := json.Marshal(m)
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	return out
}
func (s *Store) IsReady(gid int, token string) bool {
	m := s.Get(gid, token)
	return m != nil && m["status"] == "ready"
}
func (s *Store) ReadyCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, m := range s.entries {
		if m["status"] == "ready" {
			n++
		}
	}
	return n
}
func (s *Store) ListReady(page, perPage int) []eh.ArchiveInfo {
	if page < 1 {
		page = 1
	}
	if perPage <= 0 {
		perPage = 100
	}
	s.mu.RLock()
	all := []eh.ArchiveInfo{}
	for _, m := range s.entries {
		if m["status"] != "ready" {
			continue
		}
		item := eh.ArchiveInfo{GID: toInt(m["gid"]), Token: toString(m["token"]), Title: toString(m["title"]), PageCount: toInt(m["page_count"]), CreatedAt: toInt64(m["created_at"]), Quality: toString(m["quality"]), Status: "ready", MetadataAt: toInt64(m["metadata_at"])}
		if item.PageCount == 0 {
			item.PageCount = s.PageCount(item.GID, item.Token)
		}
		all = append(all, item)
	}
	s.mu.RUnlock()
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt > all[j].CreatedAt })
	start := (page - 1) * perPage
	if start >= len(all) {
		return []eh.ArchiveInfo{}
	}
	end := start + perPage
	if end > len(all) {
		end = len(all)
	}
	return all[start:end]
}
func (s *Store) PageCount(gid int, token string) int {
	p := filepath.Join(s.dir(gid, token), zipName)
	r, e := zip.OpenReader(p)
	if e != nil {
		return 0
	}
	defer r.Close()
	return len(r.File)
}
func (s *Store) GetPage(gid int, token string, page int) ([]byte, bool, error) {
	if page < 0 || !s.IsReady(gid, token) {
		return nil, false, nil
	}
	p := filepath.Join(s.dir(gid, token), zipName)
	r, e := zip.OpenReader(p)
	if e != nil {
		return nil, false, e
	}
	defer r.Close()
	if page >= len(r.File) {
		return nil, false, nil
	}
	f, e := r.File[page].Open()
	if e != nil {
		return nil, false, e
	}
	defer f.Close()
	b, e := io.ReadAll(f)
	return b, e == nil, e
}
func (s *Store) WriteMetadata(gid int, token string, payload any) error {
	b, e := json.MarshalIndent(payload, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(s.dir(gid, token), 0o755); e != nil {
		return e
	}
	tmp := filepath.Join(s.dir(gid, token), metadataName+".tmp")
	if e = os.WriteFile(tmp, b, 0o600); e != nil {
		return e
	}
	return os.Rename(tmp, filepath.Join(s.dir(gid, token), metadataName))
}
func (s *Store) ReadMetadata(gid int, token string) (map[string]any, error) {
	b, e := os.ReadFile(filepath.Join(s.dir(gid, token), metadataName))
	if e != nil {
		return nil, e
	}
	m := map[string]any{}
	e = json.Unmarshal(b, &m)
	return m, e
}
func (s *Store) Cover(gid int, token string) ([]byte, bool) {
	b, e := os.ReadFile(filepath.Join(s.dir(gid, token), coverName))
	return b, e == nil
}
func (s *Store) GetCover(gid int, token string) ([]byte, bool) { return s.Cover(gid, token) }
func (s *Store) GetMetadataSnapshot(gid int, token string) (map[string]any, bool) {
	m, e := s.ReadMetadata(gid, token)
	return m, e == nil
}
func (s *Store) WriteCover(gid int, token string, b []byte) error {
	if e := os.MkdirAll(s.dir(gid, token), 0o755); e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(s.dir(gid, token), coverName), b, 0o600)
}
func (s *Store) Remove(gid int, token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.entries[key(gid, token)]
	if os.RemoveAll(s.dir(gid, token)) != nil {
		return false
	}
	delete(s.entries, key(gid, token))
	delete(s.names, key(gid, token))
	if !ok {
		return false
	}
	return true
}
func (s *Store) Stats() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	statuses := map[string]int{}
	var bytes int64
	ready, withMeta := 0, 0
	for k, m := range s.entries {
		status := toString(m["status"])
		statuses[status]++
		if status == "ready" {
			ready++
		}
		if toInt64(m["metadata_at"]) != 0 {
			withMeta++
		}
		parts := strings.SplitN(k, ":", 2)
		if len(parts) == 2 {
			gid, _ := strconv.Atoi(parts[0])
			for _, name := range []string{zipName, partName} {
				if st, e := os.Stat(filepath.Join(s.dir(gid, parts[1]), name)); e == nil {
					bytes += st.Size()
				}
			}
		}
	}
	return map[string]any{"entries": len(s.entries), "ready": ready, "by_status": statuses, "bytes": bytes, "with_metadata": withMeta}
}
func (s *Store) Entries() []map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []map[string]any{}
	for _, m := range s.entries {
		gid := toInt(m["gid"])
		tok := toString(m["token"])
		x := cloneMap(m)
		if st, e := os.Stat(filepath.Join(s.dir(gid, tok), zipName)); e == nil {
			x["archive_size"] = st.Size()
		} else {
			x["archive_size"] = int64(0)
		}
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool { return toInt64(out[i]["created_at"]) > toInt64(out[j]["created_at"]) })
	return out
}
func toInt(v any) int { return int(toInt64(v)) }
func toInt64(v any) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	}
	return 0
}
func toString(v any) string { s, _ := v.(string); return s }
