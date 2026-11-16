package eh

import (
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
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

func TestTagSuggestionsMatchBothLanguagesAndNamespaces(t *testing.T) {
	settings := config.Settings{TagTranslationEnabled: true}
	translator := NewTagTranslator(settings, http.DefaultTransport)
	translator.install(
		map[string]string{"female": "女性", "male": "男性", "reclass": "分类"},
		map[string]string{"female:catgirl": "猫娘", "female:space tag": "空间 标签", "male:catboy": "猫少年", "reclass:Doujinshi": "同人志"},
		map[string]string{"f": "female", "m": "male"},
	)

	for _, query := range []string{"female:cat", "f:cat", "女性:猫", "女性:猫娘"} {
		got := translator.Suggestions(query, 10)
		if len(got) != 1 || got[0].Query != "female:catgirl" {
			t.Errorf("Suggestions(%q) = %+v, want female:catgirl", query, got)
		}
	}
	got := translator.Suggestions("-女性:猫", 10)
	if len(got) != 1 || got[0].Query != "-female:catgirl" {
		t.Fatalf("negative suggestion = %+v, want -female:catgirl", got)
	}
	got = translator.Suggestions("空间", 10)
	if len(got) != 1 || got[0].Query != `female:"space tag"` || got[0].Display != `女性:空间 标签 (female:"space tag")` {
		t.Fatalf("translated multiword suggestion = %+v", got)
	}
	for _, query := range []string{"同人", "Doujinshi", "分类:同人志"} {
		if got := translator.Suggestions(query, 10); len(got) != 0 {
			t.Errorf("category prefix %q returned invalid reclass suggestions: %+v", query, got)
		}
	}
}

func TestTagSuggestionsAreBoundedDeterministicAndDisabledWithTranslation(t *testing.T) {
	tags := map[string]string{}
	for i := 0; i < 20; i++ {
		tags[fmt.Sprintf("female:tag%02d", i)] = fmt.Sprintf("标签%02d", i)
	}
	settings := config.Settings{TagTranslationEnabled: true}
	translator := NewTagTranslator(settings, http.DefaultTransport)
	translator.install(map[string]string{"female": "女性"}, tags, nil)
	first := translator.Suggestions("tag", 99)
	second := translator.Suggestions("tag", 99)
	if len(first) != 10 || !reflect.DeepEqual(first, second) {
		t.Fatalf("suggestions are not bounded and deterministic: len=%d first=%+v second=%+v", len(first), first, second)
	}
	disabled := NewTagTranslator(config.Settings{TagTranslationEnabled: false}, http.DefaultTransport)
	disabled.install(map[string]string{"female": "女性"}, tags, nil)
	if got := disabled.Suggestions("tag", 10); len(got) != 0 {
		t.Fatalf("disabled translation returned suggestions: %+v", got)
	}
}
