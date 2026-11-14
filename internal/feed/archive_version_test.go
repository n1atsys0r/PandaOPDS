package feed

import (
	"strings"
	"testing"

	"pandaopds/internal/config"
	"pandaopds/internal/eh"
)

func TestArchiveVersionListKeepsStableIdentityAndUsesActiveResources(t *testing.T) {
	count := 7
	item := eh.GalleryListItem{GID: 10, Token: "old", ActiveGID: 20, ActiveToken: "new", Title: "Updated", PageCount: &count}
	b := Builder{Settings: config.Settings{EHSite: "e-hentai"}}
	atom := b.AtomList([]eh.GalleryListItem{item}, "Archives", "archives", "", "", nil)
	for _, want := range []string{
		"urn:ehentai:gallery:10:old",
		"/opds/v1.2/gallery/10/old/chapters",
		"/stream/20/new/page/{pageNumber}",
		"https://e-hentai.org/g/20/new/",
		"pse:count=\"7\"",
	} {
		if !strings.Contains(atom, want) {
			t.Fatalf("Atom list lacks %q: %s", want, atom)
		}
	}

	pub := b.Publication(item, false)
	md := pub["metadata"].(map[string]any)
	if got := md["identifier"]; got != "urn:ehentai:gallery:10:old" {
		t.Fatalf("publication identifier = %v", got)
	}
	publicationLinks := pub["links"].([]map[string]any)
	last := publicationLinks[len(publicationLinks)-1]
	if last["rel"] != RelProgression || last["href"] != "/opds/progression/20/new" || last["type"] != MIMEProgression {
		t.Fatalf("progression publication link=%#v", last)
	}
	links := pub["links"].([]map[string]any)
	if got := links[0]["href"]; got != "/stream/20/new/page/{pageNumber}" {
		t.Fatalf("direct acquisition link = %v", got)
	}
	props := links[0]["properties"].(map[string]any)
	if got := props["numberOfItems"]; got != 7 {
		t.Fatalf("active page count = %v", got)
	}
	if got := links[2]["href"]; got != "/opds/v2.0/gallery/10/old/publication" {
		t.Fatalf("self link = %v", got)
	}
}

func TestArchiveVersionDetailReadingOrderUsesActiveResources(t *testing.T) {
	b := Builder{Settings: config.Settings{PSEPageBase: 1}}
	item := eh.GalleryListItem{GID: 10, Token: "old", ActiveGID: 20, ActiveToken: "new", Title: "Updated", PageCount: intPtr(2)}
	pub := b.Publication(item, true)
	md := pub["metadata"].(map[string]any)
	if got := md["identifier"]; got != "urn:ehentai:gallery:10:old" {
		t.Fatalf("identifier = %v", got)
	}
	order := pub["readingOrder"].([]map[string]any)
	if len(order) != 2 || order[0]["href"] != "/stream/20/new/page/1" || order[1]["href"] != "/stream/20/new/page/2" {
		t.Fatalf("readingOrder = %#v", order)
	}
	links := b.DetailLinksVersion(10, "old", 20, "new", "Updated", 2)
	if links[len(links)-3]["href"] != "/opds/v2.0/gallery/10/old/publication" || links[0]["href"] != "/stream/20/new/page/{pageNumber}" || links[len(links)-1]["href"] != "/opds/progression/20/new" {
		t.Fatalf("detail links = %#v", links)
	}
	chapters := b.AtomChapterVersion(10, "old", item, "Updated", "")
	for _, want := range []string{"urn:ehentai:gallery:10:old", "/stream/20/new/page/{pageNumber}", `pse:count="2"`, `rel="http://opds-spec.org/progression"`, `type="application/opds-progression+json"`} {
		if !strings.Contains(chapters, want) {
			t.Fatalf("chapter feed lacks %q: %s", want, chapters)
		}
	}
}

func intPtr(n int) *int { return &n }
