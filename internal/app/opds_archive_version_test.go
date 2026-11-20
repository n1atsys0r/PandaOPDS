package app

import (
	"archive/zip"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchiveOPDSFeedsContainOnlyReadableLocalDocuments(t *testing.T) {
	settings := loadTestSettings(t.TempDir())
	writeTestArchive(t, filepath.Join(settings.ArchiveDir, "manual.cbz"), "page-one")
	writeTestArchive(t, filepath.Join(settings.ArchiveDir, "nested", "second.zip"), "page-two")
	server, err := New(settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	list := httptest.NewRecorder()
	server.Handler().ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/archive", nil))
	if list.Code != 200 {
		t.Fatalf("archive API status=%d %s", list.Code, list.Body.String())
	}
	var apiDoc struct {
		Entries []map[string]any `json:"entries"`
		Total   int              `json:"total"`
	}
	if err = json.Unmarshal(list.Body.Bytes(), &apiDoc); err != nil {
		t.Fatal(err)
	}
	if apiDoc.Total != 2 || len(apiDoc.Entries) != 2 {
		t.Fatalf("archive API exposed entries=%d total=%d", len(apiDoc.Entries), apiDoc.Total)
	}
	for _, entry := range apiDoc.Entries {
		if entry["gid"] != nil || entry["token"] != nil {
			t.Fatalf("unverified local archive got EH identity: %#v", entry)
		}
	}

	atom := httptest.NewRecorder()
	server.Handler().ServeHTTP(atom, httptest.NewRequest(http.MethodGet, "/opds/v1.2/archives", nil))
	if atom.Code != 200 || !strings.Contains(atom.Body.String(), "urn:pandaopds:document:") || !strings.Contains(atom.Body.String(), "/stream/documents/") {
		t.Fatalf("OPDS 1.2 local archive feed status=%d body=%s", atom.Code, atom.Body.String())
	}
	v2 := httptest.NewRecorder()
	server.Handler().ServeHTTP(v2, httptest.NewRequest(http.MethodGet, "/opds/v2.0/archives", nil))
	if v2.Code != 200 {
		t.Fatalf("OPDS 2.0 status=%d %s", v2.Code, v2.Body.String())
	}
	var feed map[string]any
	if err = json.Unmarshal(v2.Body.Bytes(), &feed); err != nil {
		t.Fatal(err)
	}
	pubs, ok := feed["publications"].([]any)
	if !ok || len(pubs) != 2 {
		t.Fatalf("OPDS 2 publications=%#v", feed["publications"])
	}
	for _, raw := range pubs {
		pub := raw.(map[string]any)
		meta := pub["metadata"].(map[string]any)
		if !strings.HasPrefix(meta["identifier"].(string), "urn:pandaopds:document:") {
			t.Fatalf("publication inferred EH identity: %#v", meta)
		}
	}
}

func writeTestArchive(t *testing.T, path, page string) {
	t.Helper()
	_ = page
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	entry, err := zw.Create("001.png")
	if err != nil {
		t.Fatal(err)
	}
	imageBytes, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/gXcAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = entry.Write(imageBytes); err != nil {
		t.Fatal(err)
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}
