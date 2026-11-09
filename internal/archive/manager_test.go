package archive

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pandaopds/internal/config"
	"pandaopds/internal/eh"
)

const mixedArchiverHTML = `<!doctype html><html><body><h1>Tier fixture</h1>
<div class="tier">
  <div>Download Cost: <strong>Free!</strong></div>
  <form action="/archiver.php?gid=1&amp;token=aa" method="post">
    <input name="dltype" value="org"><input type="submit" name="dlcheck" value="Download Original Archive">
  </form>
  <p>Estimated Size: <strong>8 MiB</strong></p>
  <p>You unlocked an original download of this archive on 2026-08-16</p>
</div>
<div class="tier">
  <div>Download Cost: <strong>315,000 GP</strong></div>
  <form action="/archiver.php?gid=1&amp;token=aa" method="post">
    <input name="dltype" value="res"><input type="submit" name="dlcheck" value="Download Resample Archive" disabled>
  </form>
  <p>Estimated Size: <strong>N/A</strong></p>
</div>
<div class="tier">
  <div>Download Cost: Unavailable</div>
  <form action="/archiver.php?gid=1&amp;token=aa" method="post">
    <input name="dltype" value="unknown"><input type="submit" name="dlcheck" value="Download Unknown Archive" disabled>
  </form>
</div>
</body></html>`

func TestParseArchiverPricesAreScopedPerTier(t *testing.T) {
	p := parseArchiver(mixedArchiverHTML, "https://e-hentai.org/archiver.php?gid=1&token=aa")
	if len(p.Options) != 3 {
		t.Fatalf("got %d options, want 3", len(p.Options))
	}
	org, res, unknown := p.Options[0], p.Options[1], p.Options[2]
	if org.Or != "org" || !org.Unlocked || !org.Available || !org.priceKnown || org.GPPrice != 0 || org.Size != "8 MiB" {
		t.Fatalf("free unlocked tier parsed incorrectly: %+v", org)
	}
	if res.Or != "res" || res.Available || !res.priceKnown || res.GPPrice != 315000 {
		t.Fatalf("paid bare-disabled tier parsed incorrectly: %+v", res)
	}
	if unknown.Or != "unknown" || unknown.Available || unknown.priceKnown {
		t.Fatalf("unknown-price disabled tier parsed incorrectly: %+v", unknown)
	}
	if price, known := parseTierPrice("Download Cost: Not Free, 9 GP maybe"); known || price != 0 {
		t.Fatalf("ambiguous cost accepted: %d, %t", price, known)
	}
}

func TestConvert7zToZIP(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "tests", "go", "testdata", "archive-sample-release.7z"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), partName)
	if err := os.WriteFile(path, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := convert7zToZIP(context.Background(), path); err != nil {
		t.Fatalf("convert7zToZIP: %v", err)
	}
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("converted archive is not zip: %v", err)
	}
	defer r.Close()
	if len(r.File) == 0 {
		t.Fatal("converted archive has no entries")
	}
	var regular int
	for _, entry := range r.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		regular++
		f, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		_, readErr := io.Copy(io.Discard, f)
		closeErr := f.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("converted entry unreadable: read=%v close=%v", readErr, closeErr)
		}
	}
	if regular == 0 {
		t.Fatal("converted archive has no readable files")
	}
}

type archiveReplay struct {
	apiStarted chan struct{}
	apiRelease chan struct{}
	page       string
	posts      atomic.Int32
	uncertain  bool
}

func (rt *archiveReplay) RoundTrip(req *http.Request) (*http.Response, error) {
	response := func(body string) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	}
	switch {
	case req.URL.Host == "e-hentai.org" && req.URL.Path == "/":
		return response("<html>ok</html>")
	case req.URL.Host == "e-hentai.org" && req.URL.Path == "/archiver.php" && req.Method == http.MethodGet:
		page := rt.page
		if page == "" {
			page = mixedArchiverHTML
		}
		return response(page)
	case req.URL.Host == "e-hentai.org" && req.URL.Path == "/archiver.php" && req.Method == http.MethodPost:
		rt.posts.Add(1)
		if rt.uncertain {
			return nil, errors.New("simulated connection reset after request write")
		}
		return response(`<html><p>Locating archive server and preparing file for download...</p><a href="https://node.hath.network/archive/1/hash/x/2">Continue</a></html>`)
	case req.URL.Host == "api.e-hentai.org" && req.URL.Path == "/api.php":
		select {
		case <-rt.apiStarted:
		default:
			close(rt.apiStarted)
		}
		select {
		case <-rt.apiRelease:
			return response(`{"gmetadata":[]}`)
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	default:
		return response("ok")
	}
}

func TestForcedStartWhileActiveDoesNotSubmitAgainAndRemoveWaits(t *testing.T) {
	temp := t.TempDir()
	replay := &archiveReplay{apiStarted: make(chan struct{}), apiRelease: make(chan struct{})}
	cfg := config.Settings{IPBMemberID: "123", IPBPassHash: "secret", EHProfile: "", EHSite: "e-hentai", TimeoutSeconds: 4, Retries: 3, CacheDir: filepath.Join(temp, "cache"), ArchiveDir: filepath.Join(temp, "archives"), ArchiveQuality: "original", ArchiveDownloadConcurrency: 1}
	service := eh.NewService(cfg, replay)
	defer service.Close()
	store, err := NewStore(cfg.ArchiveDir)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(cfg, service.Client, service, store)
	defer manager.Close()

	first, err := manager.Start(context.Background(), 1, "aa", "original", false)
	if err != nil {
		t.Fatal(err)
	}
	if first["status"] != "pending" {
		t.Fatalf("first start status = %v", first["status"])
	}
	select {
	case <-replay.apiStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("archive worker did not reach metadata request")
	}
	second, err := manager.Start(context.Background(), 1, "aa", "resample", true)
	if err != nil {
		t.Fatal(err)
	}
	if !second["active"].(bool) || replay.posts.Load() != 1 {
		t.Fatalf("force overlapped the active job: status=%v posts=%d", second, replay.posts.Load())
	}

	removed := make(chan bool, 1)
	go func() { removed <- manager.Remove(1, "aa") }()
	select {
	case ok := <-removed:
		if !ok {
			t.Fatal("active archive entry was not removed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("remove did not cancel and wait for the archive worker")
	}
	if got := manager.Status(1, "aa")["status"]; got != "absent" {
		t.Fatalf("status after removal = %v", got)
	}
}

func TestUncertainSubmitIsNotRetried(t *testing.T) {
	temp := t.TempDir()
	replay := &archiveReplay{apiStarted: make(chan struct{}), apiRelease: make(chan struct{}), uncertain: true}
	cfg := config.Settings{IPBMemberID: "123", IPBPassHash: "secret", EHSite: "e-hentai", TimeoutSeconds: 4, Retries: 5, CacheDir: filepath.Join(temp, "cache"), ArchiveDir: filepath.Join(temp, "archives"), ArchiveQuality: "original"}
	service := eh.NewService(cfg, replay)
	defer service.Close()
	store, err := NewStore(cfg.ArchiveDir)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(cfg, service.Client, service, store)
	defer manager.Close()
	if _, err := manager.Start(context.Background(), 1, "aa", "original", false); err == nil {
		t.Fatal("uncertain transport error unexpectedly accepted")
	}
	if got := replay.posts.Load(); got != 1 {
		t.Fatalf("archive submit attempts = %d, want 1", got)
	}
	if got := store.Get(1, "aa")["status"]; got != "failed" {
		t.Fatalf("uncertain submit status = %v, want failed", got)
	}
}

func TestStartRefusesUnknownTierPriceBeforeSubmit(t *testing.T) {
	temp := t.TempDir()
	page := `<html><body><h1>Unpriced</h1><div><div>Download Cost: Contact support</div><form action="/archiver.php"><input name="dltype" value="org"><input name="dlcheck" value="Download Original Archive"></form></div></body></html>`
	replay := &archiveReplay{apiStarted: make(chan struct{}), apiRelease: make(chan struct{}), page: page}
	cfg := config.Settings{IPBMemberID: "123", IPBPassHash: "secret", EHSite: "e-hentai", TimeoutSeconds: 4, Retries: 3, CacheDir: filepath.Join(temp, "cache"), ArchiveDir: filepath.Join(temp, "archives"), ArchiveQuality: "original"}
	service := eh.NewService(cfg, replay)
	defer service.Close()
	store, err := NewStore(cfg.ArchiveDir)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(cfg, service.Client, service, store)
	defer manager.Close()
	if _, err := manager.Start(context.Background(), 1, "aa", "original", false); err == nil || !strings.Contains(err.Error(), "unable to determine") {
		t.Fatalf("unknown tier price error = %v", err)
	}
	if got := replay.posts.Load(); got != 0 {
		t.Fatalf("unknown-price tier was submitted %d times", got)
	}
}

func TestInterruptedTierUpgradeRestoresReadyArchiveOnRestart(t *testing.T) {
	temp := t.TempDir()
	cfg := config.Settings{EHSite: "e-hentai", TimeoutSeconds: 2, CacheDir: filepath.Join(temp, "cache"), ArchiveDir: filepath.Join(temp, "archives")}
	store, err := NewStore(cfg.ArchiveDir)
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(store.dir(9, "upgrade"), zipName)
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	entry, err := zw.Create("page-1.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("old ready bytes")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	backup := map[string]any{"or": "res", "quality": "Resample", "gp_price": 300, "download_url": "old-url", "page_count": 1, "total_bytes": int64(200)}
	if _, err := store.Upsert(9, "upgrade", map[string]any{"status": "downloading", "or": "org", "quality": "Original", "upgrade_backup": backup}); err != nil {
		t.Fatal(err)
	}
	part := filepath.Join(store.dir(9, "upgrade"), partName)
	if err := os.WriteFile(part, []byte("new partial tier"), 0o600); err != nil {
		t.Fatal(err)
	}

	restartedStore, err := NewStore(cfg.ArchiveDir)
	if err != nil {
		t.Fatal(err)
	}
	service := eh.NewService(cfg, nil)
	defer service.Close()
	manager := NewManager(cfg, service.Client, service, restartedStore)
	defer manager.Close()
	if rolled := manager.RecoverInterruptedUpgrades(); rolled != 1 {
		t.Fatalf("recovered upgrades=%d, want 1", rolled)
	}
	status := manager.Status(9, "upgrade")
	if status["status"] != "ready" || status["or"] != "res" || status["quality"] != "Resample" || status["error"] != "upgrade interrupted by restart; rolled back" {
		t.Fatalf("restart did not restore old ready tier: %#v", status)
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Fatalf("cross-tier partial survived rollback: stat err=%v", err)
	}
	if restartedStore.PageCount(9, "upgrade") != 1 {
		t.Fatal("old archive bytes did not remain readable after rollback")
	}
}
