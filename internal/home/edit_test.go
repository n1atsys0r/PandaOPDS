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
