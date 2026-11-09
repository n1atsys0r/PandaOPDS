package eh

import (
	"net/http"
	"path/filepath"
	"testing"

	"pandaopds/internal/config"
)

func TestTagTranslatorOfflineSnapshotAndQueryParity(t *testing.T) {
	dir := t.TempDir()
	settings := config.Settings{TimeoutSeconds: 1, TagTranslationState: filepath.Join(dir, "translation.json"), TagTranslationEnabled: true, Facets: []config.Facet{{Name: "Doujinshi", Mask: 1021}}}
	translator := NewTagTranslator(settings, http.DefaultTransport)
	db := []byte(`{"data":[
		{"namespace":"female","frontMatters":{"name":"女性","abbr":"f","rank":1},"data":{"catgirl":{"name":"猫娘"},"space tag":{"name":"空间 标签"}}},
		{"namespace":"male","frontMatters":{"name":"男性","abbr":"m"},"data":{"duplicate":{"name":"重名"}}},
		{"namespace":"mixed","frontMatters":{"name":"混合","abbr":"x"},"data":{"duplicate":{"name":"重名"}}},
		{"namespace":"reclass","frontMatters":{"name":"分类","abbr":"r"},"data":{"Doujinshi":{"name":"同人志"}}}
	]}`)
	names, tags, abbrs, err := parseTranslationDB(db)
	if err != nil {
		t.Fatal(err)
	}
	translator.install(names, tags, abbrs)
	if got := translator.TranslateQuery(`女性:猫娘 重名 "rating:5"`); got != `female:catgirl 重名 "rating:5"` {
		t.Fatalf("prefixed/ambiguous/quoted rewrite = %q", got)
	}
	if got := translator.TranslateQuery(`猫娘`); got != `female:catgirl` {
		t.Fatalf("unique bare translation = %q", got)
	}
	if got := translator.TranslateQuery(`女性:空间 标签`); got != `female:"space tag"` {
		t.Fatalf("multiword prefixed rewrite = %q", got)
	}
	if got := translator.TranslateQuery(`女性:unknown`); got != `female:unknown` {
		t.Fatalf("namespace alias rewrite = %q", got)
	}
	if got := translator.NormalizeCategoryQuery(`分类:同人志`, settings.Facets); got != `category:Doujinshi` {
		t.Fatalf("translated category rewrite = %q", got)
	}
	if got, ok := translator.DisplayTag("f", "catgirl"); !ok || got != "女性:猫娘" {
		t.Fatalf("display tag = %q, %v", got, ok)
	}
	if got := translator.DisplayCategory("Doujinshi"); got != "分类:同人志" {
		t.Fatalf("display category = %q", got)
	}
	loaded := NewTagTranslator(settings, http.DefaultTransport)
	if got, ok := loaded.DisplayTag("female", "catgirl"); !ok || got != "女性:猫娘" {
		t.Fatalf("persisted translation failed to reload: %q, %v", got, ok)
	}
	translator.install(names, map[string]string{
		"female:zulu": "同名", "female:alpha": "同名",
	}, abbrs)
	if got := translator.TranslateQuery("女性:同名"); got != "female:alpha" {
		t.Fatalf("duplicate translation lookup is not deterministic: %q", got)
	}
}

func TestCleanTranslationRemovesEmojiAndCollapsesSpaces(t *testing.T) {
	if got := cleanTranslation("  猫🐱  娘 "); got != "猫 娘" {
		t.Fatalf("clean translation = %q", got)
	}
}

func TestTranslationOutputRespectsDisabledSetting(t *testing.T) {
	tag := NewTagTranslator(config.Settings{TagTranslationEnabled: false}, http.DefaultTransport)
	if got := tag.DisplayCategory("Manga"); got != "category:Manga" {
		t.Fatalf("disabled category output = %q", got)
	}
	if got, ok := tag.DisplayTag("language", "japanese"); ok || got != "language:japanese" {
		t.Fatalf("disabled tag output = %q, %v", got, ok)
	}
}
