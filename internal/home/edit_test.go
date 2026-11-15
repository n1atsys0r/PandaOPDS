package home

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveMakesBackupAndPreservesNilCount(t *testing.T) {
	p := filepath.Join(t.TempDir(), "home.toml")
	old := []byte(`[[group]]
id = "browse"
title = "Browse"

[[section]]
group = "browse"
kind = "navigation"
type = "preset"
query = "latest"
`)
	if err := os.WriteFile(p, old, 0600); err != nil {
		t.Fatal(err)
	}
	c, err := DecodeEditor(old)
	if err != nil || len(c.Sections) != 1 || !c.Sections[0].Group {
		t.Fatalf("legacy group was not converted: %+v, %v", c, err)
	}
	if err := SaveSnapshot(p, c, true, old); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(p + ".bak")
	if err != nil || string(backup) != string(old) {
		t.Fatalf("backup mismatch: %v", err)
	}
	encoded, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("[[group]]")) || !bytes.Contains(encoded, []byte("group = true")) {
		t.Fatalf("save did not use boolean grouping: %s", encoded)
	}
	got, err := DecodeEditor(encoded)
	if err != nil || len(got.Sections) != 1 || !got.Sections[0].Group || got.Sections[0].Count != nil {
		t.Fatalf("omitted count changed: %+v %v", got, err)
	}
}
func TestValidateRejectsMultipleRootPublicationsAndNegativeCount(t *testing.T) {
	n := -1
	c := Config{Sections: []Section{{Kind: "publication", Type: "preset", Query: "latest", Count: &n}}}
	if Validate(c) == nil {
		t.Fatal("expected negative count rejection")
	}
	c.Sections = []Section{{Kind: "publication", Type: "preset", Query: "latest"}, {Kind: "publication", Type: "preset", Query: "popular"}}
	if Validate(c) == nil {
		t.Fatal("expected multiple root publication rejection")
	}
}

func TestDecodeEditorPreservesOmittedAndZeroCount(t *testing.T) {
	c, err := DecodeEditor([]byte(`[[section]]
kind="publication"
type="preset"
query="latest"
title="All"

[[section]]
kind="publication"
type="preset"
query="popular"
title="Zero"
count=0
`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Sections[0].Count != nil {
		t.Fatalf("omitted count became %v", *c.Sections[0].Count)
	}
	if c.Sections[1].Count == nil || *c.Sections[1].Count != 0 {
		t.Fatalf("explicit zero was not preserved: %+v", c.Sections[1].Count)
	}
}

func TestRuntimeLoadIgnoresUnknownFieldsButEditorRejects(t *testing.T) {
	p := filepath.Join(t.TempDir(), "home.toml")
	data := []byte("[[section]]\nkind=\"navigation\"\ntype=\"preset\"\nquery=\"latest\"\ntitle=\"Latest\"\nfuture_field=\"legacy extension\"\n")
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil || len(c.Sections) != 1 || c.Sections[0].Query != "latest" {
		t.Fatalf("runtime compatibility changed: %+v %v", c, err)
	}
	if _, err := DecodeEditor(data); err == nil {
		t.Fatal("editor should report unknown persisted fields")
	}
}

func TestExampleHomeConfigParsesAndValidates(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "home.toml.example"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := DecodeEditor(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = Validate(c); err != nil {
		t.Fatal(err)
	}
}

func TestValidateTopLevelPublicationAndGroupsAreExclusive(t *testing.T) {
	root := Section{Kind: "publication", Type: "preset", Query: "latest"}
	topNavigation := Section{Kind: "navigation", Type: "preset", Query: "popular"}
	groupedNavigation := Section{Kind: "navigation", Group: true, Type: "preset", Query: "popular"}
	groupedPublication := Section{Kind: "publication", Group: true, Type: "preset", Query: "popular"}
	for _, grouped := range []Section{groupedNavigation, groupedPublication} {
		if err := Validate(Config{Sections: []Section{root, topNavigation, grouped}}); err == nil {
			t.Fatalf("accepted root publication with grouped section %+v", grouped)
		}
	}
	if err := Validate(Config{Sections: []Section{root, topNavigation}}); err != nil {
		t.Fatalf("top-level navigation should coexist with root publication: %v", err)
	}
	if err := Validate(Config{Sections: []Section{groupedNavigation, topNavigation}}); err != nil {
		t.Fatalf("grouped section should coexist with root navigation: %v", err)
	}
}

func TestNavigationGroupWithMultipleItems(t *testing.T) {
	data := []byte(`[[section]]
kind = "navigation"
group = true
title = "Rankings"

[[section.items]]
title = "Month"
type = "preset"
query = "toplist:month"

[[section.items]]
title = "Year"
type = "preset"
query = "toplist:year"
`)
	c, err := DecodeEditor(data)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(c.Sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(c.Sections))
	}
	sec := c.Sections[0]
	if !sec.Group || sec.Kind != "navigation" || sec.Title != "Rankings" {
		t.Fatalf("unexpected section header: %+v", sec)
	}
	if len(sec.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(sec.Items))
	}
	if sec.Items[0].Title != "Month" || sec.Items[0].Query != "toplist:month" {
		t.Fatalf("item 0 mismatch: %+v", sec.Items[0])
	}
	if sec.Items[1].Title != "Year" || sec.Items[1].Query != "toplist:year" {
		t.Fatalf("item 1 mismatch: %+v", sec.Items[1])
	}
	if err := Validate(c); err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	// Test invalid item query fails validation
	bad := Config{Sections: []Section{{
		Kind:  "navigation",
		Group: true,
		Title: "Bad Group",
		Items: []NavigationItem{{Title: "Bad", Type: "preset", Query: "invalid_preset"}},
	}}}
	if err := Validate(bad); err == nil {
		t.Fatal("expected validation error for invalid preset in items")
	}

	// Test strict mode rejects unknown fields in items
	badStrict := []byte(`[[section]]
kind = "navigation"
group = true
title = "Rankings"

[[section.items]]
title = "Month"
type = "preset"
query = "toplist:month"
unknown_field = "oops"
`)
	if _, err := DecodeEditor(badStrict); err == nil {
		t.Fatal("expected DecodeEditor to reject unknown field in section.items")
	}
}

func TestMultiPageConfigDecodeAndValidate(t *testing.T) {
	data := []byte(`
[[page]]
id = "home"
title = "首页"

[[page.section]]
kind = "navigation"
title = "同人志专区"
type = "page"
query = "doujin"

[[page.section]]
kind = "publication"
title = "热门"
type = "preset"
query = "popular"
count = 10

[[page]]
id = "doujin"
title = "同人志"

[[page.section]]
kind = "navigation"
title = "返回首页"
type = "page"
query = "home"

[[page.section]]
kind = "publication"
title = "中文同人"
type = "search"
query = "language:chinese"
count = 20
`)
	c, err := DecodeEditor(data)
	if err != nil {
		t.Fatalf("failed to decode multi-page: %v", err)
	}
	if len(c.Pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(c.Pages))
	}
	if err := Validate(c); err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	homePage, ok := c.FindPage("home")
	if !ok || homePage.Title != "首页" {
		t.Fatalf("home page not found or mismatch: %+v", homePage)
	}
	if len(homePage.Sections) != 2 {
		t.Fatalf("expected 2 sections in home, got %d", len(homePage.Sections))
	}

	doujinPage, ok := c.FindPage("doujin")
	if !ok || doujinPage.Title != "同人志" {
		t.Fatalf("doujin page not found or mismatch: %+v", doujinPage)
	}
	if len(doujinPage.Sections) != 2 {
		t.Fatalf("expected 2 sections in doujin, got %d", len(doujinPage.Sections))
	}

	// Test BuildHref
	if href := BuildHref("page", "home"); href != "/opds/v2.0" {
		t.Fatalf("expected /opds/v2.0 for page home, got %s", href)
	}
	if href := BuildHref("page", "doujin"); href != "/opds/v2.0/page/doujin" {
		t.Fatalf("expected /opds/v2.0/page/doujin, got %s", href)
	}
	if href := BuildHref("preset", "toplists"); href != "/opds/v2.0/page/toplists" {
		t.Fatalf("expected /opds/v2.0/page/toplists, got %s", href)
	}
	// Test ResolvePreset toplists and page: alias
	if kind, arg, err := ResolvePreset("toplists"); err != nil || kind != "toplists" || arg != "" {
		t.Fatalf("ResolvePreset(toplists) failed: kind=%s, arg=%s, err=%v", kind, arg, err)
	}
	if kind, arg, err := ResolvePreset("page:doujin"); err != nil || kind != "page" || arg != "doujin" {
		t.Fatalf("ResolvePreset(page:doujin) failed: kind=%s, arg=%s, err=%v", kind, arg, err)
	}

	// Test validation: duplicate ID
	dupConfig := c
	dupConfig.Pages = append(dupConfig.Pages, Page{ID: "doujin", Title: "Duplicate", Sections: nil})
	if err := Validate(dupConfig); err == nil {
		t.Fatalf("expected error for duplicate page ID, got nil")
	}

	// Test validation: missing home
	noHome := Config{Pages: []Page{{ID: "custom", Title: "Custom", Sections: nil}}}
	if err := Validate(noHome); err == nil {
		t.Fatalf("expected error for missing home page, got nil")
	}

	// Test validation: invalid slug
	invalidSlug := Config{Pages: []Page{{ID: "home", Title: "Home"}, {ID: "bad slug!", Title: "Bad"}}}
	if err := Validate(invalidSlug); err == nil {
		t.Fatalf("expected error for invalid slug, got nil")
	}
}

func TestDefaultPresetConfiguration(t *testing.T) {
	c := Default()
	if err := Validate(c); err != nil {
		t.Fatalf("Default() must pass Validate(): %v", err)
	}

	if len(c.Pages) != 1 || c.Pages[0].ID != "home" {
		t.Fatalf("expected 1 page ('home'), got %d", len(c.Pages))
	}

	// Verify 4 top navigation items globally
	navs := c.GlobalNavigation()
	if len(navs) != 4 {
		t.Fatalf("global navigation has %d items, want 4", len(navs))
	}
	for j, navID := range []string{"watched", "favorites", "toplists", "archives"} {
		if navs[j].Type != "preset" || navs[j].Query != navID {
			t.Errorf("global nav[%d] = {%s, %s}, want {preset, %s}", j, navs[j].Type, navs[j].Query, navID)
		}
	}

	// Page: home (sections mode, Popular > Latest)
	homeP := c.HomePage()
	if homeP.Mode != "groups" {
		t.Errorf("home page mode=%s, want groups", homeP.Mode)
	}
	var homePubs []Section
	for _, s := range homeP.Sections {
		if s.Kind == "publication" && s.Group {
			homePubs = append(homePubs, s)
		}
	}
	if len(homePubs) != 2 || homePubs[0].Query != "popular" || homePubs[1].Query != "latest" {
		t.Errorf("home publications = %+v, want Popular then Latest", homePubs)
	}

	// Page: watched (publications mode, Watched publication)
	watchedP, _ := c.FindPage("watched")
	if watchedP.Mode != "publications" {
		t.Errorf("watched page mode=%s, want publications", watchedP.Mode)
	}
	hasWatchedPub := false
	for _, s := range watchedP.Sections {
		if s.Kind == "publication" && !s.Group && s.Query == "watched" {
			hasWatchedPub = true
		}
	}
	if !hasWatchedPub {
		t.Errorf("watched page missing publication: pub=%v", hasWatchedPub)
	}

	// Page: favorites (publications mode, Favorites publication)
	favP, _ := c.FindPage("favorites")
	if favP.Mode != "publications" {
		t.Errorf("favorites page mode=%s, want publications", favP.Mode)
	}
	hasFavPub := false
	for _, s := range favP.Sections {
		if s.Kind == "publication" && !s.Group && s.Query == "favorites" {
			hasFavPub = true
		}
	}
	if !hasFavPub {
		t.Errorf("favorites page missing publication: pub=%v", hasFavPub)
	}

	// Page: toplists (sections mode, Yesterday > Past Month > Past Year > All-Time)
	topP, _ := c.FindPage("toplists")
	if topP.Mode != "groups" {
		t.Errorf("toplists page mode=%s, want groups", topP.Mode)
	}
	var topPubs []Section
	for _, s := range topP.Sections {
		if s.Kind == "publication" && s.Group {
			topPubs = append(topPubs, s)
		}
	}
	expectedPeriods := []string{"toplist:yesterday", "toplist:month", "toplist:year", "toplist:alltime"}
	if len(topPubs) != 4 {
		t.Fatalf("toplists page has %d groups, want 4", len(topPubs))
	}
	for i, q := range expectedPeriods {
		if topPubs[i].Query != q {
			t.Errorf("toplists[%d] query=%s, want %s", i, topPubs[i].Query, q)
		}
	}

	// Page: archives (publications mode, Archives publication)
	archP, _ := c.FindPage("archives")
	if archP.Mode != "publications" {
		t.Errorf("archives page mode=%s, want publications", archP.Mode)
	}
	hasArchPub := false
	for _, s := range archP.Sections {
		if s.Kind == "publication" && !s.Group && s.Query == "archives" {
			hasArchPub = true
		}
	}
	if !hasArchPub {
		t.Errorf("archives page missing publication: pub=%v", hasArchPub)
	}
}

func TestCustomFacetsValidationAndDecode(t *testing.T) {
	data := []byte(`
[[page]]
id = "home"
title = "Home"
mode = "publications"

  [[page.section]]
  kind = "publication"
  type = "preset"
  query = "latest"

  [[page.section]]
  kind = "facets"
  title = "精选筛选"

    [[page.section.items]]
    title = "全部"

    [[page.section.items]]
    title = "4星汉化同人"
    categories = ["Doujinshi"]
    rating = 4
    expunged = true
    disable_filters = ["language"]
    query = "language:chinese"
`)
	c, err := DecodeEditor(data)
	if err != nil {
		t.Fatalf("DecodeEditor failed: %v", err)
	}
	if err := Validate(c); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	facetsSec := c.HomePage().Sections[1]
	if facetsSec.Kind != "facets" || facetsSec.Title != "精选筛选" {
		t.Fatalf("unexpected section: %+v", facetsSec)
	}
	if len(facetsSec.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(facetsSec.Items))
	}
	item2 := facetsSec.Items[1]
	if item2.Title != "4星汉化同人" || item2.Rating != 4 || !item2.Expunged || len(item2.Categories) != 1 || item2.Categories[0] != "Doujinshi" || item2.Query != "language:chinese" {
		t.Fatalf("unexpected item: %+v", item2)
	}

	// Test invalid item (rating > 5)
	badRating := facetsSec
	badRating.Items[1].Rating = 6
	if err := ValidateSection(badRating); err == nil {
		t.Fatal("expected error for rating > 5")
	}

	// Test invalid item (empty title)
	badTitle := facetsSec
	badTitle.Items[1].Title = ""
	if err := ValidateSection(badTitle); err == nil {
		t.Fatal("expected error for empty title")
	}
}

func TestCheckConfigHomeToml(t *testing.T) {
	b, err := os.ReadFile("../../config/home.toml")
	if err != nil {
		t.Skip("no config/home.toml")
	}
	c, err := DecodeEditor(b)
	if err != nil {
		t.Fatalf("DecodeEditor failed on config/home.toml: %v", err)
	}
	if err := Validate(c); err != nil {
		t.Fatalf("Validate failed on config/home.toml: %v", err)
	}
}

func TestReferencedFacetSets(t *testing.T) {
	tomlContent := `
[[facet_set]]
id = "chinese_filter"
title = "中文与同人"

[[facet_set.items]]
title = "全部"

[[facet_set.items]]
title = "高分同人"
categories = ["Doujinshi"]
rating = 4
expunged = true
disable_filters = ["language"]
query = "language:chinese"

[[page]]
id = "home"
title = "首页"
mode = "publications"

[[page.section]]
kind = "publication"
type = "preset"
query = "latest"

[[page.section]]
kind = "facets"
facet_set = "chinese_filter"
`
	c, err := decodeHome([]byte(tomlContent), false)
	if err != nil {
		t.Fatalf("decodeHome failed: %v", err)
	}
	if err := Validate(c); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if len(c.FacetSets) != 1 {
		t.Fatalf("expected 1 facet set, got %d", len(c.FacetSets))
	}
	fs := c.GetFacetSet("chinese_filter")
	if fs == nil || fs.Title != "中文与同人" || len(fs.Items) != 2 {
		t.Fatalf("unexpected facet set: %+v", fs)
	}
	sec := c.HomePage().Sections[1]
	if sec.Kind != "facets" || sec.FacetSet != "chinese_filter" {
		t.Fatalf("unexpected section: %+v", sec)
	}

	// Save snapshot and reload
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "home.toml")
	if err := SaveSnapshot(path, c, false, nil); err != nil {
		t.Fatalf("SaveSnapshot failed: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := decodeHome(b, false)
	if err != nil {
		t.Fatalf("reloaded decodeHome failed: %v", err)
	}
	if err := Validate(reloaded); err != nil {
		t.Fatalf("reloaded Validate failed: %v", err)
	}
	if len(reloaded.GetSearchFacets()) != 2 || reloaded.GetSearchFacets()[0].Title != "全部" {
		t.Fatalf("reloaded search facets mismatch: %+v", reloaded.GetSearchFacets())
	}

	// Unknown facet_set validation failure
	badConfig := c
	badConfig.Pages[0].Sections[1].FacetSet = "non_existent"
	if err := Validate(badConfig); err == nil {
		t.Fatal("expected error for unknown facet set reference, got nil")
	}
}
