package migration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"pandaopds/internal/archive"
	"pandaopds/internal/storage"
)

func TestImportLegacySidecarsPreservesUnknownMetadataCoverAndSource(t *testing.T) {
	base := t.TempDir()
	archiveDir := filepath.Join(base, "files")
	stateDir := filepath.Join(base, "state")
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		t.Fatal(err)
	}
	s, err := archive.NewStoreWithState(archiveDir, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Catalog(47, "TokenA", map[string]any{"title": "from list"}, map[string]any{"title": "Old title", "category": "Manga", "extension": map[string]any{"unknown": "kept"}})
	if err != nil {
		t.Fatal(err)
	}
	cover := []byte("legacy cover bytes")
	if err = os.WriteFile(filepath.Join(stateDir, "47", "cover.jpg"), cover, 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(stateDir, "47", "state.json")
	galleryPath := filepath.Join(stateDir, "47", "gallery.json")
	beforeState, _ := os.ReadFile(statePath)
	beforeGallery, _ := os.ReadFile(galleryPath)
	beforeCover, _ := os.ReadFile(filepath.Join(stateDir, "47", "cover.jpg"))
	beforeIndex, _ := os.ReadFile(filepath.Join(stateDir, "index.snapshot"))
	db, err := storage.Open(filepath.Join(base, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = ImportLegacyArchive(db, archiveDir, stateDir); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err = db.QueryRow(`SELECT metadata_json FROM publications WHERE provider='eh' AND gid=47 AND token='tokena'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if raw == "{}" || json.Unmarshal([]byte(raw), &decoded) != nil || decoded["extension"].(map[string]any)["unknown"] != "kept" {
		t.Fatalf("gallery metadata raw=%s", raw)
	}
	var title, category, uploader string
	var pageCount int
	if err = db.QueryRow(`SELECT title,category,uploader,page_count FROM publications WHERE provider='eh' AND gid=47 AND token='tokena'`).Scan(&title, &category, &uploader, &pageCount); err != nil {
		t.Fatal(err)
	}
	if title != "Old title" || category != "Manga" {
		t.Fatalf("structured imported fields title=%q category=%q", title, category)
	}
	var got []byte
	var source string
	if err = db.QueryRow(`SELECT data,source FROM covers c JOIN publications p ON p.id=c.publication_id WHERE p.gid=47 AND p.token='tokena'`).Scan(&got, &source); err != nil {
		t.Fatal(err)
	}
	if string(got) != string(cover) || source != "legacy_sidecar" {
		t.Fatalf("cover=%q source=%q", got, source)
	}
	afterState, _ := os.ReadFile(statePath)
	afterGallery, _ := os.ReadFile(galleryPath)
	afterCover, _ := os.ReadFile(filepath.Join(stateDir, "47", "cover.jpg"))
	afterIndex, _ := os.ReadFile(filepath.Join(stateDir, "index.snapshot"))
	if string(beforeState) != string(afterState) || string(beforeGallery) != string(afterGallery) || string(beforeCover) != string(afterCover) || string(beforeIndex) != string(afterIndex) {
		t.Fatal("legacy files changed during import")
	}
}
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
