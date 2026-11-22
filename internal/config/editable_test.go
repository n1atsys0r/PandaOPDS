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
	if err := WriteEditableFile(path, map[string]any{"EH_SITE": "exhentai", "PSE_PAGE_BASE": float64(0), "COMMENTS_ENABLED": false}); err != nil {
		t.Fatal(err)
	}
	s := LoadWithConfigPath(path)
	if s.EHSite != "exhentai" || s.PSEPageBase != 0 || s.CommentsEnabled {
		t.Fatalf("file overrides not loaded: %+v", s)
	}
	t.Setenv("EH_SITE", "e-hentai")
	s = LoadWithConfigPath(path)
	if s.EHSite != "e-hentai" {
		t.Fatalf("environment did not override file: %q", s.EHSite)
	}
}

func TestRetiredFavoritesSyncSettingsAreIgnored(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.toml")
	if err := os.WriteFile(p, []byte("FAVORITES_SYNC_ARCHIVE = true\nFAVORITES_SYNC_CATEGORIES = \"1,-0\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readEditableFile(p); err != nil {
		t.Fatalf("retired favorites settings should be ignored: %v", err)
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

func TestRetiredAcquisitionSettingIsIgnoredAndDoesNotBreakOtherSettings(t *testing.T) {
	t.Setenv("OPDS_ACQ_DETAIL", "")
	_ = os.Unsetenv("OPDS_ACQ_DETAIL")
	t.Setenv("EH_SITE", "")
	_ = os.Unsetenv("EH_SITE")
	p := filepath.Join(t.TempDir(), "settings.toml")
	if err := os.WriteFile(p, []byte("OPDS_ACQ_DETAIL = true\nEH_SITE = \"exhentai\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := LoadWithConfigPath(p).EHSite; got != "exhentai" {
		t.Fatalf("legacy setting prevented remaining settings from loading: EH_SITE=%q", got)
	}
	if IsEditableKey("OPDS_ACQ_DETAIL") {
		t.Fatal("retired setting is still editable")
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
