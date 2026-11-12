package eh

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"pandaopds/internal/config"
)

type switchedArchiveFixture struct{}

func (switchedArchiveFixture) GetPage(int, string, int) ([]byte, bool, error) { return nil, false, nil }
func (switchedArchiveFixture) ReadyCount() int                                { return 1 }
func (switchedArchiveFixture) ListReady(int, int) []ArchiveInfo {
	return []ArchiveInfo{{GID: 10, Token: "old", ActiveGID: 20, ActiveToken: "new", PageCount: 3}}
}
func (switchedArchiveFixture) GetCover(int, string) ([]byte, bool) { return nil, false }
func (switchedArchiveFixture) GetMetadataSnapshot(gid int, token string) (map[string]any, bool) {
	if gid != 20 || token != "new" {
		return nil, false
	}
	return map[string]any{"title": "Active title", "title_jpn": "Active title", "category": "Manga", "posted": 1700000000}, true
}
func (switchedArchiveFixture) PageCount(gid int, token string) int {
	if gid == 20 && token == "new" {
		return 3
	}
	return 0
}
func (switchedArchiveFixture) ResolveActive(gid int, token string) (int, string) {
	if gid == 10 && token == "old" {
		return 20, "new"
	}
	return gid, token
}

type noArchiveNetwork struct{}

func (noArchiveNetwork) RoundTrip(*http.Request) (*http.Response, error) {
	panic("switched archive detail must not make an upstream request")
}

type unavailableReadyArchive struct{ switchedArchiveFixture }

func (unavailableReadyArchive) GetPage(int, string, int) ([]byte, bool, error) {
	return nil, false, errors.New("archive volume unavailable")
}
func (unavailableReadyArchive) IsReady(int, string) bool { return true }

func TestReadyArchiveReadFailureReturnsTemporaryUnavailable(t *testing.T) {
	s := NewService(config.Settings{PSEPageBase: 1}, noArchiveNetwork{})
	defer s.Close()
	s.AttachArchive(unavailableReadyArchive{})
	_, _, err := s.GetImage(context.Background(), 10, "old", 1)
	var upstream *UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != 503 {
		t.Fatalf("error=%v, want local archive 503", err)
	}
}

func TestSwitchedArchiveDetailUsesActiveSnapshotWithoutUpstream(t *testing.T) {
	s := NewService(config.Settings{PageURLTTLSeconds: 60}, noArchiveNetwork{})
	defer s.Close()
	s.AttachArchive(switchedArchiveFixture{})
	d, err := s.GetDetail(context.Background(), 10, "old")
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Active title" || d.ImageCount != 3 || d.Category != "Manga" {
		t.Fatalf("detail = %#v", d)
	}
}

func TestArchivedGalleriesKeepRootIdentityAndActiveVersion(t *testing.T) {
	s := NewService(config.Settings{}, noArchiveNetwork{})
	defer s.Close()
	s.AttachArchive(switchedArchiveFixture{})
	items := s.ArchivedGalleries(1)
	if len(items) != 1 {
		t.Fatalf("archived item count = %d", len(items))
	}
	item := items[0]
	if item.GID != 10 || item.Token != "old" || item.ActiveGID != 20 || item.ActiveToken != "new" {
		t.Fatalf("archive identities = %#v", item)
	}
	if item.Title != "Active title" || item.PageCount == nil || *item.PageCount != 3 {
		t.Fatalf("archive metadata = %#v", item)
	}
}
