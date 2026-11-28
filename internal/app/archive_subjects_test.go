package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pandaopds/internal/config"
	"pandaopds/internal/document"
	"pandaopds/internal/eh"
	"pandaopds/internal/feed"
	"pandaopds/internal/storage"
)

// writeTranslationState writes an EhTagTranslation snapshot so a translator can
// be built without network access.
func writeTranslationState(t *testing.T, base string) config.Settings {
	t.Helper()
	settings := config.Settings{
		TagTranslationEnabled: true,
		TagTranslationState:   filepath.Join(base, "translation.json"),
	}
	state, err := json.Marshal(map[string]any{
		"namespaces": map[string]string{"female": "女性", "reclass": "分类"},
		"abbrs":      map[string]string{"f": "female"},
		"tags":       map[string]string{"female:catgirl": "猫娘", "reclass:Manga": "漫画"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(settings.TagTranslationState, state, 0o600); err != nil {
		t.Fatal(err)
	}
	return settings
}

// linkedArchiveWithTags registers one local ZIP associated with a gallery whose
// publication row carries a category and one EhTagTranslation tag.
func linkedArchiveWithTags(t *testing.T, base string, settings config.Settings) (*document.Store, string) {
	t.Helper()
	archiveRoot := filepath.Join(base, "archive")
	writeTestArchive(t, filepath.Join(archiveRoot, "translated.cbz"), "page")
	db, err := storage.Open(filepath.Join(base, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	docs, err := document.New(db, archiveRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = docs.ScanDeep(context.Background()); err != nil {
		t.Fatal(err)
	}
	id := mustFirstDocumentID(t, docs)
	if err = docs.Associate(context.Background(), id, 4242, "abc123"); err != nil {
		t.Fatal(err)
	}
	metadata := `{"title":"Linked","category":"Manga","tags":{"female":[{"key":"catgirl","status":"confidence"}]}}`
	tagsJSON := `{"female":[{"key":"catgirl","status":"confidence"}]}`
	if _, err = db.Exec(`UPDATE publications SET metadata_json=?,category='Manga',tags_json=? WHERE provider='eh' AND gid=4242`, metadata, tagsJSON); err != nil {
		t.Fatal(err)
	}
	return docs, id
}

func translatedBuilder(t *testing.T, settings config.Settings) feed.Builder {
	t.Helper()
	translator := eh.NewTagTranslator(settings, nil)
	t.Cleanup(translator.Stop)
	return feed.Builder{Settings: settings, Translator: translator}
}

// The local archive shelf must translate subjects exactly like the gallery
// feeds do when TAG_TRANSLATION_ENABLED is on.
func TestArchiveFeedTranslatesSubjects(t *testing.T) {
	base := t.TempDir()
	settings := writeTranslationState(t, base)
	docs, _ := linkedArchiveWithTags(t, base, settings)
	builder := translatedBuilder(t, settings)

	doc, _, err := DocumentArchiveOPDSFeed(context.Background(), docs, builder, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	subjects := doc["publications"].([]map[string]any)[0]["metadata"].(map[string]any)["subject"]
	raw, err := json.Marshal(subjects)
	if err != nil {
		t.Fatal(err)
	}
	names := string(raw)
	for _, want := range []string{"分类:漫画", "女性:猫娘"} {
		if !strings.Contains(names, want) {
			t.Fatalf("archive shelf subject=%s, want translated entry %q", names, want)
		}
	}
	if strings.Contains(names, "female:catgirl") || strings.Contains(names, "category:Manga") {
		t.Fatalf("archive shelf kept untranslated subjects: %s", names)
	}
}

// Translation off must keep native ns:key subjects on the archive shelf.
func TestArchiveFeedKeepsNativeSubjectsWhenTranslationDisabled(t *testing.T) {
	base := t.TempDir()
	settings := writeTranslationState(t, base)
	settings.TagTranslationEnabled = false
	docs, _ := linkedArchiveWithTags(t, base, settings)
	builder := translatedBuilder(t, settings)

	doc, _, err := DocumentArchiveOPDSFeed(context.Background(), docs, builder, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	subjects := doc["publications"].([]map[string]any)[0]["metadata"].(map[string]any)["subject"]
	raw, _ := json.Marshal(subjects)
	names := string(raw)
	if !strings.Contains(names, "female:catgirl") || !strings.Contains(names, "category:Manga") {
		t.Fatalf("disabled translation changed archive shelf subjects: %s", names)
	}
	if strings.Contains(names, "女性") || strings.Contains(names, "分类") {
		t.Fatalf("disabled translation still emitted translated subjects: %s", names)
	}
}

// The document-ID manifest is a local reading entry and must translate too.
func TestDocumentManifestTranslatesSubjects(t *testing.T) {
	base := t.TempDir()
	settings := writeTranslationState(t, base)
	settings.PSEPageBase = 1
	docs, id := linkedArchiveWithTags(t, base, settings)
	builder := translatedBuilder(t, settings)

	mux := http.NewServeMux()
	RegisterDocumentRoutes(mux, docs, builder)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/opds/v2.0/documents/"+id+"/manifest", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("manifest status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, want := range []string{"女性:猫娘", "分类:漫画"} {
		if !strings.Contains(body, want) {
			t.Fatalf("document manifest missing translated subject %q: %s", want, body)
		}
	}
}

// The archive manifest fallback path (upstream unavailable) must translate as
// well, so an offline archive still shows Chinese subjects.
func TestArchiveManifestFallbackTranslatesSubjects(t *testing.T) {
	base := t.TempDir()
	settings := writeTranslationState(t, base)
	settings.PSEPageBase = 1
	settings.EHSite = "e-hentai"
	settings.TimeoutSeconds = 2
	settings.Retries = 0
	settings.CacheDir = filepath.Join(base, "cache")
	docs, _ := linkedArchiveWithTags(t, base, settings)

	service := eh.NewService(settings, unavailableGalleryTransport{})
	t.Cleanup(func() { service.Close() })
	server := &Server{Settings: settings, Service: service, Documents: docs}

	request := httptest.NewRequest(http.MethodGet, "/opds/v2.0/gallery/4242/abc123/manifest", nil)
	request.SetPathValue("gid", "4242")
	request.SetPathValue("token", "abc123")
	recorder := httptest.NewRecorder()
	server.manifest20(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("fallback manifest status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, want := range []string{"女性:猫娘", "分类:漫画"} {
		if !strings.Contains(body, want) {
			t.Fatalf("fallback manifest missing translated subject %q: %s", want, body)
		}
	}
}
