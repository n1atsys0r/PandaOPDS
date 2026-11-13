package feed

import (
	"testing"

	"pandaopds/internal/config"
	"pandaopds/internal/eh"
)

func TestPublicationIncludesExtraMetadata(t *testing.T) {
	count := 42
	item := eh.GalleryListItem{
		GID:       123456,
		Token:     "abcdef",
		Title:     "[Author] Example Gallery",
		Category:  "Doujinshi",
		Rating:    4.5,
		Uploader:  "testuser",
		PageCount: &count,
	}
	b := Builder{Settings: config.Settings{EHSite: "e-hentai"}}
	pub := b.Publication(item, false)
	md, ok := pub["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("expected metadata in publication")
	}

	if md["x:gid"] != 123456 {
		t.Fatalf("expected x:gid 123456, got %v", md["x:gid"])
	}
	if md["x:token"] != "abcdef" {
		t.Fatalf("expected x:token abcdef, got %v", md["x:token"])
	}
	if md["x:category"] != "Doujinshi" {
		t.Fatalf("expected x:category Doujinshi, got %v", md["x:category"])
	}
	if md["x:rating"] != 4.5 {
		t.Fatalf("expected x:rating 4.5, got %v", md["x:rating"])
	}
	if md["x:uploader"] != "testuser" {
		t.Fatalf("expected x:uploader testuser, got %v", md["x:uploader"])
	}
}
