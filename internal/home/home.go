package home

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Section struct {
	Group bool   `toml:"group"`
	Kind  string `toml:"kind"`
	Title string `toml:"title"`
	Type  string `toml:"type"`
	Query string `toml:"query"`
	Count *int   `toml:"count"`
}
type Config struct {
	Sections []Section `toml:"section"`
}

func Default() Config {
	return Config{Sections: []Section{
		{Group: true, Kind: "publication", Title: "Toplist: Yesterday", Type: "preset", Query: "toplist:yesterday", Count: intp(20)},
		{Group: true, Kind: "navigation", Title: "Toplist: Month", Type: "preset", Query: "toplist:month"},
		{Group: true, Kind: "navigation", Title: "Toplist: Year", Type: "preset", Query: "toplist:year"},
		{Group: true, Kind: "publication", Title: "Popular", Type: "preset", Query: "popular", Count: intp(20)},
		{Group: true, Kind: "navigation", Title: "Latest", Type: "preset", Query: "latest"},
		{Group: true, Kind: "publication", Title: "Chinese Doujinshi", Type: "search", Query: "language:chinese", Count: intp(20)},
		{Kind: "navigation", Title: "Toplist: All Time", Type: "preset", Query: "toplist:alltime"},
		{Kind: "navigation", Title: "Watched", Type: "preset", Query: "watched"},
		{Kind: "navigation", Title: "Favorites", Type: "preset", Query: "favorites"},
		{Kind: "navigation", Title: "Japanese Original", Type: "search", Query: "language:japanese"},
		{Kind: "navigation", Title: "Archives", Type: "preset", Query: "archives"},
	}}
}
func intp(v int) *int { return &v }

func Validate(c Config) error {
	var rootPublication *Section
	groupedSection := false
	for _, s := range c.Sections {
		if s.Kind != "publication" && s.Kind != "navigation" && s.Kind != "facets" {
			return fmt.Errorf("invalid section kind %q", s.Kind)
		}
		if s.Type != "preset" && s.Type != "search" {
			return fmt.Errorf("invalid section type %q", s.Type)
		}
		if s.Type == "preset" {
			if _, _, err := ResolvePreset(s.Query); err != nil {
				return err
			}
		} else if s.Query == "" {
			return fmt.Errorf("search query cannot be empty")
		}
		if s.Count != nil && *s.Count < 0 {
			return fmt.Errorf("section count must be nonnegative")
		}
		if s.Kind == "publication" && !s.Group {
			if rootPublication != nil {
				return fmt.Errorf("only one ungrouped publication section is allowed")
			}
			rootPublication = &s
		}
		if s.Group {
			groupedSection = true
		}
		if s.Kind == "facets" && s.Group {
			return fmt.Errorf("facet sections cannot be grouped")
		}
		if s.Kind == "facets" && s.Type == "preset" {
			kind, _, _ := ResolvePreset(s.Query)
			if kind != "search" && kind != "toplist" && kind != "favorites" {
				return fmt.Errorf("preset has no facet provider: %s", s.Query)
			}
		}
	}
	if rootPublication != nil && groupedSection {
		return fmt.Errorf("top-level publications and groups cannot coexist")
	}
	for _, s := range c.Sections {
		if s.Kind == "facets" && (rootPublication == nil || rootPublication.Type != s.Type || rootPublication.Query != s.Query) {
			return fmt.Errorf("facet source must match an ungrouped publication section")
		}
	}
	return nil
}
func SaveSnapshot(path string, c Config, hadPrevious bool, previous []byte) error {
	if err := Validate(c); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	if hadPrevious {
		if e := atomicWrite(path+".bak", previous); e != nil {
			return e
		}
	}
	return atomicWrite(path, b)
}
func atomicWrite(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".home-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0o600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func Load(path string) (Config, error) {
	if path == "" {
		return Default(), nil
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return Default(), nil
	}
	c, e := decodeHome(b, false)
	if e != nil {
		return Default(), e
	}
	c = normalize(c)
	if err := Validate(c); err != nil {
		return Default(), err
	}
	return c, nil
}
func normalize(c Config) Config {
	for i := range c.Sections {
		s := &c.Sections[i]
		if s.Kind == "" {
			s.Kind = "publication"
		}
		if s.Type == "" {
			s.Type = "preset"
		}
		if s.Query == "" {
			s.Query = "latest"
		}
		if s.Count == nil {
			n := 0
			if s.Kind == "publication" {
				n = 10
			}
			s.Count = &n
		}
	}
	return c
}

func DecodeEditor(b []byte) (Config, error) {
	raw, err := decodeHome(b, true)
	if err != nil {
		return Default(), err
	}
	counts := make([]*int, len(raw.Sections))
	for i, s := range raw.Sections {
		if s.Count != nil {
			n := *s.Count
			counts[i] = &n
		}
	}
	c := normalize(raw)
	for i := range c.Sections {
		c.Sections[i].Count = counts[i]
	}
	return c, nil
}
func decodeHome(b []byte, strict bool) (Config, error) {
	var data struct {
		Groups []struct {
			ID    string `toml:"id"`
			Title string `toml:"title"`
		} `toml:"group"`
		Sections []map[string]any `toml:"section"`
	}
	metadata, err := toml.Decode(string(b), &data)
	if err != nil {
		return Default(), fmt.Errorf("invalid home configuration TOML")
	}
	if strict && len(metadata.Undecoded()) > 0 {
		return Default(), fmt.Errorf("unknown home configuration field")
	}
	legacy := len(data.Groups) > 0
	for _, m := range data.Sections {
		if _, ok := m["group"].(string); ok {
			legacy = true
		}
	}
	c := Config{Sections: make([]Section, 0, len(data.Sections))}
	for _, m := range data.Sections {
		if strict {
			for k := range m {
				if k != "group" && k != "kind" && k != "title" && k != "type" && k != "query" && k != "count" {
					return Default(), fmt.Errorf("unknown home configuration field")
				}
			}
		}
		for key, value := range m {
			switch key {
			case "group":
				if _, ok := value.(bool); !ok {
					if _, legacyString := value.(string); !legacyString {
						return Default(), fmt.Errorf("invalid group value")
					}
				}
			case "kind", "title", "type", "query":
				if _, ok := value.(string); !ok {
					return Default(), fmt.Errorf("invalid section field type")
				}
			case "count":
				if _, ok := value.(int64); !ok {
					return Default(), fmt.Errorf("invalid section count")
				}
			}
		}
		s := Section{Kind: "publication", Type: "preset", Query: "latest"}
		if v, ok := m["kind"].(string); ok {
			s.Kind = v
		}
		if v, ok := m["title"].(string); ok {
			s.Title = v
		}
		if v, ok := m["type"].(string); ok {
			s.Type = v
		}
		if v, ok := m["query"].(string); ok {
			s.Query = v
		}
		if v, ok := m["count"].(int64); ok {
			n := int(v)
			s.Count = &n
		}
		if v, ok := m["group"].(bool); ok {
			s.Group = v
		} else if _, ok := m["group"].(string); ok {
			s.Group = m["group"] != "" || s.Kind == "publication"
		} else if legacy {
			s.Group = s.Kind == "publication"
		}
		c.Sections = append(c.Sections, s)
	}
	return c, nil
}

func ResolvePreset(q string) (kind, arg string, err error) {
	switch q {
	case "latest":
		return "search", "", nil
	case "popular", "watched", "favorites", "archives":
		return q, "", nil
	}
	if strings.HasPrefix(q, "favorites:") {
		arg = strings.TrimPrefix(q, "favorites:")
		if arg == "" {
			return "", "", fmt.Errorf("unknown favorites favcat")
		}
		for _, r := range arg {
			if r < '0' || r > '9' {
				return "", "", fmt.Errorf("unknown favorites favcat: %s", arg)
			}
		}
		return "favorites", arg, nil
	}
	if strings.HasPrefix(q, "toplist:") {
		arg = strings.TrimPrefix(q, "toplist:")
		switch arg {
		case "yesterday", "month", "year", "alltime":
			return "toplist", arg, nil
		}
		return "", "", fmt.Errorf("unknown toplist period: %s", arg)
	}
	return "", "", fmt.Errorf("unknown preset: %s", q)
}
func BuildHref(typ, query string) string {
	base := "/opds/v2.0"
	if typ == "search" {
		return base + "/gallery?query=" + url.QueryEscape(query)
	}
	kind, arg, e := ResolvePreset(query)
	if e != nil {
		return base + "/gallery"
	}
	switch kind {
	case "search":
		return base + "/gallery"
	case "popular", "watched", "favorites":
		if arg != "" {
			return base + "/gallery?query=favorites:" + arg
		}
		return base + "/gallery?query=" + kind
	case "toplist":
		return base + "/toplist?period=" + arg
	case "archives":
		return base + "/archives"
	}
	return base + "/gallery"
}
func AuthRequired(s Section) bool {
	return s.Type == "preset" && (s.Query == "watched" || s.Query == "favorites" || strings.HasPrefix(s.Query, "favorites:"))
}
