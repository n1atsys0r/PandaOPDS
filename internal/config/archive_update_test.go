package config

import (
	"path/filepath"
	"testing"
)

func TestArchiveUpdateDefaultsAndEnvironment(t *testing.T) {
	t.Setenv("ARCHIVE_UPDATE_INTERVAL_DAYS", "1")
	t.Setenv("ARCHIVE_AUTO_UPDATE", "true")
	t.Setenv("ARCHIVE_UPDATE_INTERVAL_SECONDS", "0")
	s := loadWithEditableValues(nil)
	if s.ArchiveUpdateIntervalSeconds != 7*86400 || s.ArchiveAutoUpdate {
		t.Fatalf("defaults: interval=%d auto=%v", s.ArchiveUpdateIntervalSeconds, s.ArchiveAutoUpdate)
	}
	path := filepath.Join(t.TempDir(), "settings.toml")
	values := map[string]any{"ARCHIVE_UPDATE_INTERVAL_DAYS": int64(3), "ARCHIVE_AUTO_UPDATE": true}
	if err := WriteEditableFile(path, values); err != nil {
		t.Fatal(err)
	}
	raw, err := readEditableFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s = LoadWithEditableValues(raw)
	if s.ArchiveUpdateIntervalSeconds != 3*86400 || !s.ArchiveAutoUpdate {
		t.Fatalf("file values: interval=%d auto=%v", s.ArchiveUpdateIntervalSeconds, s.ArchiveAutoUpdate)
	}
	for _, value := range []any{float64(1.5), float64(-1), float64(archiveMaxDays() + 1), float64(1e100)} {
		if ValidateEditableValue("ARCHIVE_UPDATE_INTERVAL_DAYS", value) == nil {
			t.Fatalf("accepted invalid days value %v", value)
		}
	}
}
