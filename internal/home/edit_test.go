package home

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveMakesBackupAndPreservesNilCount(t *testing.T) {
	p := filepath.Join(t.TempDir(), "home.toml")
	old := []byte("[[group]]\nid = \"browse\"\ntitle = \"Browse\"\n\n[[section]]\nkind = \"navigation\"\ntitle = \"Latest\"\ntype = \"preset\"\nquery = \"latest\"\n")
	if err := os.WriteFile(p, old, 0600); err != nil {
		t.Fatal(err)
	}
	c := Config{Groups: []Group{{ID: "browse", Title: "Browse"}}, Sections: []Section{{Group: "browse", Kind: "navigation", Title: "Latest", Type: "preset", Query: "latest"}}}
	if err := SaveSnapshot(p, c, true, old); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(p + ".bak")
	if err != nil || string(backup) != string(old) {
		t.Fatalf("backup mismatch: %v %q", err, backup)
	}
	encoded, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeEditor(encoded)
	if err != nil || len(got.Sections) != 1 || got.Sections[0].Count != nil {
		t.Fatalf("omitted count changed: %+v, %v", got, err)
	}
}

func TestValidateRejectsMissingGroupAndNegativeCount(t *testing.T) {
	n := -1
	c := Config{Groups: []Group{{ID: "g"}}, Sections: []Section{{Group: "missing", Kind: "publication", Type: "preset", Query: "latest", Count: &n}}}
	if Validate(c) == nil {
		t.Fatal("expected invalid group rejection")
	}
	c.Sections[0].Group = "g"
	if Validate(c) == nil {
		t.Fatal("expected negative count rejection")
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
