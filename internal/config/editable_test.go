package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEditableSettingsFileAndEnvironmentPrecedence(t *testing.T) {
	for _, key := range editableKeys {
		t.Setenv(key, "")
		_ = os.Unsetenv(key)
	}
	t.Setenv("OPDS_ACQ_MODE", "")
	_ = os.Unsetenv("OPDS_ACQ_MODE")
	path := filepath.Join(t.TempDir(), "settings.toml")
	if err := WriteEditableFile(path, map[string]any{"EH_SITE": "exhentai", "PSE_PAGE_BASE": float64(0), "COMMENTS_ENABLED": false, "FAVORITES_SYNC_ARCHIVE": true, "FAVORITES_SYNC_CATEGORIES": "1,-0"}); err != nil {
		t.Fatal(err)
	}
	s := LoadWithConfigPath(path)
	if s.EHSite != "exhentai" || s.PSEPageBase != 0 || s.CommentsEnabled || !s.FavoritesSyncArchive || len(s.FavoritesSyncCategories) != 1 || s.FavoritesSyncCategories[0] != 1 || len(s.FavoritesSyncExcludes) != 1 || s.FavoritesSyncExcludes[0] != 0 {
		t.Fatalf("file overrides not loaded: %+v", s)
	}
	t.Setenv("EH_SITE", "e-hentai")
	s = LoadWithConfigPath(path)
	if s.EHSite != "e-hentai" {
		t.Fatalf("environment did not override file: %q", s.EHSite)
	}
}

func TestFavoritesSyncCategoriesEnvironmentOverridesFile(t *testing.T) {
	t.Setenv("FAVORITES_SYNC_CATEGORIES", "3,-5")
	p := filepath.Join(t.TempDir(), "settings.toml")
	if err := WriteEditableFile(p, map[string]any{"FAVORITES_SYNC_CATEGORIES": "1,-0"}); err != nil {
		t.Fatal(err)
	}
	s := LoadWithConfigPath(p)
	if len(s.FavoritesSyncCategories) != 1 || s.FavoritesSyncCategories[0] != 3 || len(s.FavoritesSyncExcludes) != 1 || s.FavoritesSyncExcludes[0] != 5 {
		t.Fatalf("environment did not override favorites scope: %+v", s)
	}
}

func TestFavoritesSyncCategoriesValidation(t *testing.T) {
	for _, value := range []string{"", "0,1,-5", " 2 , -0 "} {
		if err := ValidateEditableValue("FAVORITES_SYNC_CATEGORIES", value); err != nil {
			t.Fatalf("valid category scope %q rejected: %v", value, err)
		}
	}
	for _, value := range []string{"10", "-", "x", "1,,11"} {
		if err := ValidateEditableValue("FAVORITES_SYNC_CATEGORIES", value); err == nil {
			t.Fatalf("invalid category scope %q accepted", value)
		}
	}
}

func TestFavoritesSyncArchiveEnvironmentOverridesFile(t *testing.T) {
	t.Setenv("FAVORITES_SYNC_ARCHIVE", "false")
	p := filepath.Join(t.TempDir(), "settings.toml")
	if err := WriteEditableFile(p, map[string]any{"FAVORITES_SYNC_ARCHIVE": true}); err != nil {
		t.Fatal(err)
	}
	if got := LoadWithConfigPath(p).FavoritesSyncArchive; got {
		t.Fatal("environment variable did not override favorites archive setting")
	}
}

func TestEditableSettingsFileRejectsUnknownAndKeepsMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.toml")
	if err := os.WriteFile(p, []byte("EH_SITE = \"exhentai\"\nUNKNOWN = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readEditableFile(p); err == nil {
		t.Fatal("expected unknown key error")
	}
}

func TestIPBCredentialsAreAtomicAcrossEnvironmentAndFile(t *testing.T) {
	t.Setenv("IPB_MEMBER_ID", "")
	_ = os.Unsetenv("IPB_MEMBER_ID")
	t.Setenv("IPB_PASS_HASH", "")
	_ = os.Unsetenv("IPB_PASS_HASH")
	p := filepath.Join(t.TempDir(), "settings.toml")
	if err := WriteEditableFile(p, map[string]any{"IPB_MEMBER_ID": "file-member", "IPB_PASS_HASH": "file-hash"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IPB_MEMBER_ID", "env-member")
	s := LoadWithConfigPath(p)
	if s.IPBMemberID != "env-member" || s.IPBPassHash != "" {
		t.Fatalf("mixed credential sources: member=%q hash=%q", s.IPBMemberID, s.IPBPassHash)
	}
}

func TestLegacyAcquisitionEnvironmentOverridesFile(t *testing.T) {
	t.Setenv("OPDS_ACQ_DETAIL", "")
	_ = os.Unsetenv("OPDS_ACQ_DETAIL")
	t.Setenv("OPDS_ACQ_MODE", "direct")
	p := filepath.Join(t.TempDir(), "settings.toml")
	if err := WriteEditableFile(p, map[string]any{"OPDS_ACQ_DETAIL": true}); err != nil {
		t.Fatal(err)
	}
	if got := LoadWithConfigPath(p).OPDSAcqDetail; got {
		t.Fatal("legacy direct env did not override file")
	}
}

func TestPublicBaseURLValidation(t *testing.T) {
	for _, v := range []string{"", "https://reader.example/proxy/path"} {
		if err := ValidateEditableValue("PUBLIC_BASE_URL", v); err != nil {
			t.Fatalf("valid URL %q rejected: %v", v, err)
		}
	}
	for _, v := range []string{"/proxy", "ftp://reader.example", "https://u:p@reader.example", "https://reader.example?x=1", "https://reader.example#fragment"} {
		if err := ValidateEditableValue("PUBLIC_BASE_URL", v); err == nil {
			t.Fatalf("invalid URL %q accepted", v)
		}
	}
}
