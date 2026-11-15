package home

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

var slugRE = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

type NavigationItem struct {
	Title string `toml:"title" json:"title"`
	Type  string `toml:"type" json:"type"`
	Query string `toml:"query" json:"query"`
}

type Section struct {
	Group bool             `toml:"group" json:"group"`
	Kind  string           `toml:"kind" json:"kind"`
	Title string           `toml:"title" json:"title"`
	Type  string           `toml:"type" json:"type"`
	Query string           `toml:"query" json:"query"`
	Count *int             `toml:"count" json:"count"`
	Items []NavigationItem `toml:"items,omitempty" json:"items,omitempty"`
}

type Page struct {
	ID       string    `toml:"id" json:"id"`
	Title    string    `toml:"title" json:"title"`
	Mode     string    `toml:"mode,omitempty" json:"mode,omitempty"`
	Sections []Section `toml:"section" json:"sections"`
}

type Config struct {
	Pages    []Page    `toml:"page" json:"pages"`
	Sections []Section `toml:"section,omitempty" json:"sections,omitempty"`
}

func Default() Config {
	topNav := []Section{
		{Kind: "navigation", Title: "Watched", Type: "page", Query: "watched"},
		{Kind: "navigation", Title: "Favorites", Type: "page", Query: "favorites"},
		{Kind: "navigation", Title: "Toplists", Type: "page", Query: "toplists"},
		{Kind: "navigation", Title: "Archives", Type: "page", Query: "archives"},
	}

	homePage := Page{
		ID:    "home",
		Title: "Home",
		Mode:  "groups",
		Sections: []Section{
			topNav[0], topNav[1], topNav[2], topNav[3],
			{Group: true, Kind: "publication", Title: "Popular", Type: "preset", Query: "popular", Count: intp(20)},
			{Group: true, Kind: "publication", Title: "Latest", Type: "preset", Query: "latest", Count: intp(20)},
		},
	}

	watchedPage := Page{
		ID:    "watched",
		Title: "Watched",
		Mode:  "publications",
		Sections: []Section{
			topNav[0], topNav[1], topNav[2], topNav[3],
			{Group: false, Kind: "publication", Title: "Watched", Type: "preset", Query: "watched", Count: intp(20)},
			{Group: false, Kind: "facets", Title: "Categories", Type: "preset", Query: "watched"},
		},
	}

	favoritesPage := Page{
		ID:    "favorites",
		Title: "Favorites",
		Mode:  "publications",
		Sections: []Section{
			topNav[0], topNav[1], topNav[2], topNav[3],
			{Group: false, Kind: "publication", Title: "Favorites", Type: "preset", Query: "favorites", Count: intp(20)},
			{Group: false, Kind: "facets", Title: "Folders", Type: "preset", Query: "favorites"},
		},
	}

	toplistsPage := Page{
		ID:    "toplists",
		Title: "Toplists",
		Mode:  "groups",
		Sections: []Section{
			topNav[0], topNav[1], topNav[2], topNav[3],
			{Group: true, Kind: "publication", Title: "All Time", Type: "preset", Query: "toplist:alltime", Count: intp(20)},
			{Group: true, Kind: "publication", Title: "Past Year", Type: "preset", Query: "toplist:year", Count: intp(20)},
			{Group: true, Kind: "publication", Title: "Past Month", Type: "preset", Query: "toplist:month", Count: intp(20)},
			{Group: true, Kind: "publication", Title: "Yesterday", Type: "preset", Query: "toplist:yesterday", Count: intp(20)},
		},
	}

	archivesPage := Page{
		ID:    "archives",
		Title: "Archives",
		Mode:  "publications",
		Sections: []Section{
			topNav[0], topNav[1], topNav[2], topNav[3],
			{Group: false, Kind: "publication", Title: "Archives", Type: "preset", Query: "archives", Count: intp(20)},
			{Group: false, Kind: "facets", Title: "Categories", Type: "preset", Query: "archives"},
		},
	}

	pages := []Page{homePage, watchedPage, favoritesPage, toplistsPage, archivesPage}
	return Config{
		Pages:    pages,
		Sections: homePage.Sections,
	}
}
func intp(v int) *int { return &v }

func (c Config) HomePage() Page {
	for _, p := range c.Pages {
		if p.ID == "home" {
			return p
		}
	}
	if len(c.Pages) > 0 {
		return c.Pages[0]
	}
	return Page{ID: "home", Title: "Home", Sections: c.Sections}
}

func (c Config) FindPage(id string) (Page, bool) {
	for _, p := range c.Pages {
		if p.ID == id {
			return p, true
		}
	}
	if id == "home" && len(c.Sections) > 0 {
		return Page{ID: "home", Title: "Home", Sections: c.Sections}, true
	}
	return Page{}, false
}

func ValidateSection(s Section) error {
	if s.Kind != "publication" && s.Kind != "navigation" && s.Kind != "facets" {
		return fmt.Errorf("invalid section kind %q", s.Kind)
	}
	if s.Kind == "navigation" {
		if s.Group && len(s.Items) > 0 {
			for _, it := range s.Items {
				if it.Type != "preset" && it.Type != "search" && it.Type != "page" {
					return fmt.Errorf("invalid navigation item type %q", it.Type)
				}
				if it.Type == "preset" {
					if _, _, err := ResolvePreset(it.Query); err != nil {
						return err
					}
				} else if it.Type == "page" {
					if it.Query == "" || !slugRE.MatchString(it.Query) {
						return fmt.Errorf("invalid target page id %q", it.Query)
					}
				} else if it.Query == "" {
					return fmt.Errorf("navigation item search query cannot be empty")
				}
			}
		} else {
			if s.Type != "preset" && s.Type != "search" && s.Type != "page" {
				return fmt.Errorf("invalid section type %q", s.Type)
			}
			if s.Type == "preset" {
				if _, _, err := ResolvePreset(s.Query); err != nil {
					return err
				}
			} else if s.Type == "page" {
				if s.Query == "" || !slugRE.MatchString(s.Query) {
					return fmt.Errorf("invalid target page id %q", s.Query)
				}
			} else if s.Query == "" {
				return fmt.Errorf("search query cannot be empty")
			}
		}
	} else {
		// publication or facets
		if s.Type != "preset" && s.Type != "search" {
			return fmt.Errorf("invalid section type %q for kind %s", s.Type, s.Kind)
		}
		if s.Type == "preset" {
			if _, _, err := ResolvePreset(s.Query); err != nil {
				return err
			}
		} else if s.Query == "" {
			return fmt.Errorf("search query cannot be empty")
		}
	}
	if s.Count != nil && *s.Count < 0 {
		return fmt.Errorf("section count must be nonnegative")
	}
	if s.Kind == "facets" && s.Group {
		return fmt.Errorf("facet sections cannot be grouped")
	}
	if s.Kind == "facets" && s.Type == "preset" {
		kind, _, _ := ResolvePreset(s.Query)
		if kind != "search" && kind != "toplist" && kind != "favorites" && kind != "watched" && kind != "archives" {
			return fmt.Errorf("preset has no facet provider: %s", s.Query)
		}
	}
	return nil
}

func ValidatePage(p Page) error {
	if p.ID == "" || !slugRE.MatchString(p.ID) {
		return fmt.Errorf("invalid page id %q (must be alphanumeric, dashes, or underscores)", p.ID)
	}
	if strings.TrimSpace(p.Title) == "" {
		return fmt.Errorf("page %q title cannot be empty", p.ID)
	}
	if p.Mode != "" && p.Mode != "publications" && p.Mode != "sections" && p.Mode != "groups" {
		return fmt.Errorf("page %q: invalid mode %q", p.ID, p.Mode)
	}
	var rootPublication *Section
	groupedSection := false
	for i := range p.Sections {
		s := p.Sections[i]
		if err := ValidateSection(s); err != nil {
			return fmt.Errorf("page %q: %w", p.ID, err)
		}
		if s.Kind == "publication" && !s.Group {
			if rootPublication != nil {
				return fmt.Errorf("page %q: only one ungrouped publication section is allowed", p.ID)
			}
			rootPublication = &p.Sections[i]
		}
		if s.Group {
			groupedSection = true
		}
	}
	if rootPublication != nil && groupedSection {
		return fmt.Errorf("page %q: top-level publications and groups cannot coexist", p.ID)
	}
	for _, s := range p.Sections {
		if s.Kind == "facets" && (rootPublication == nil || rootPublication.Type != s.Type || rootPublication.Query != s.Query) {
			return fmt.Errorf("page %q: facet source must match an ungrouped publication section", p.ID)
		}
	}
	return nil
}

func Validate(c Config) error {
	pages := c.Pages
	if len(pages) == 0 && len(c.Sections) > 0 {
		pages = []Page{{ID: "home", Title: "首页", Sections: c.Sections}}
	}
	if len(pages) == 0 {
		return nil
	}
	hasHome := false
	seenIDs := make(map[string]bool)
	for _, p := range pages {
		if seenIDs[p.ID] {
			return fmt.Errorf("duplicate page id %q", p.ID)
		}
		seenIDs[p.ID] = true
		if p.ID == "home" {
			hasHome = true
		}
		if err := ValidatePage(p); err != nil {
			return err
		}
	}
	if !hasHome {
		return fmt.Errorf("configuration must include a 'home' page")
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
	toSave := c
	if len(toSave.Pages) == 0 && len(toSave.Sections) > 0 {
		toSave.Pages = []Page{{ID: "home", Title: "首页", Sections: toSave.Sections}}
	}
	// Do not write top-level sections if Pages are present
	toSave.Sections = nil
	b, err := toml.Marshal(toSave)
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
func normalizeSections(sections []Section) []Section {
	for i := range sections {
		s := &sections[i]
		if s.Kind == "" {
			s.Kind = "publication"
		}
		if s.Kind == "navigation" && s.Group {
			if len(s.Items) == 0 && s.Query != "" {
				s.Items = []NavigationItem{{Title: s.Title, Type: s.Type, Query: s.Query}}
			}
			for j := range s.Items {
				it := &s.Items[j]
				if it.Type == "" {
					it.Type = "preset"
				}
				if it.Query == "" {
					it.Query = "latest"
				}
			}
			if len(s.Items) > 0 {
				if s.Type == "" {
					s.Type = s.Items[0].Type
				}
				if s.Query == "" {
					s.Query = s.Items[0].Query
				}
			}
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
	return sections
}

func normalize(c Config) Config {
	if len(c.Pages) == 0 && len(c.Sections) > 0 {
		c.Pages = []Page{{ID: "home", Title: "Home", Sections: c.Sections}}
	}
	for i := range c.Pages {
		c.Pages[i].Sections = normalizeSections(c.Pages[i].Sections)
		if c.Pages[i].Title == "" {
			if c.Pages[i].ID == "home" {
				c.Pages[i].Title = "Home"
			} else {
				c.Pages[i].Title = c.Pages[i].ID
			}
		}
	}
	c.Sections = c.HomePage().Sections
	return c
}

func DecodeEditor(b []byte) (Config, error) {
	raw, err := decodeHome(b, true)
	if err != nil {
		return Default(), err
	}
	// Preserve raw count pointers for each section in each page
	counts := make([][]*int, len(raw.Pages))
	for pIdx, p := range raw.Pages {
		counts[pIdx] = make([]*int, len(p.Sections))
		for sIdx, s := range p.Sections {
			if s.Count != nil {
				n := *s.Count
				counts[pIdx][sIdx] = &n
			}
		}
	}
	c := normalize(raw)
	for pIdx := range c.Pages {
		if pIdx < len(counts) {
			for sIdx := range c.Pages[pIdx].Sections {
				if sIdx < len(counts[pIdx]) {
					c.Pages[pIdx].Sections[sIdx].Count = counts[pIdx][sIdx]
				}
			}
		}
	}
	c.Sections = c.HomePage().Sections
	return c, nil
}

type rawSection struct {
	Group any              `toml:"group"`
	Kind  any              `toml:"kind"`
	Title any              `toml:"title"`
	Type  any              `toml:"type"`
	Query any              `toml:"query"`
	Count any              `toml:"count"`
	Items []NavigationItem `toml:"items"`
}

func decodeRawSections(raw []rawSection, legacy bool) ([]Section, error) {
	sections := make([]Section, 0, len(raw))
	for _, m := range raw {
		if m.Group != nil {
			if _, ok := m.Group.(bool); !ok {
				if _, legacyString := m.Group.(string); !legacyString {
					return nil, fmt.Errorf("invalid group value")
				}
			}
		}
		if m.Kind != nil {
			if _, ok := m.Kind.(string); !ok {
				return nil, fmt.Errorf("invalid section field type")
			}
		}
		if m.Title != nil {
			if _, ok := m.Title.(string); !ok {
				return nil, fmt.Errorf("invalid section field type")
			}
		}
		if m.Type != nil {
			if _, ok := m.Type.(string); !ok {
				return nil, fmt.Errorf("invalid section field type")
			}
		}
		if m.Query != nil {
			if _, ok := m.Query.(string); !ok {
				return nil, fmt.Errorf("invalid section field type")
			}
		}
		if m.Count != nil {
			if _, ok := m.Count.(int64); !ok {
				return nil, fmt.Errorf("invalid section count")
			}
		}
		s := Section{Kind: "publication", Type: "preset", Query: "latest"}
		if v, ok := m.Kind.(string); ok {
			s.Kind = v
		}
		if v, ok := m.Title.(string); ok {
			s.Title = v
		}
		if v, ok := m.Type.(string); ok {
			s.Type = v
		}
		if v, ok := m.Query.(string); ok {
			s.Query = v
		}
		if v, ok := m.Count.(int64); ok {
			n := int(v)
			s.Count = &n
		}
		if len(m.Items) > 0 {
			s.Items = append(s.Items, m.Items...)
		}
		if v, ok := m.Group.(bool); ok {
			s.Group = v
		} else if _, ok := m.Group.(string); ok {
			s.Group = m.Group != "" || s.Kind == "publication"
		} else if legacy {
			s.Group = s.Kind == "publication"
		}
		sections = append(sections, s)
	}
	return sections, nil
}

func decodeHome(b []byte, strict bool) (Config, error) {
	var data struct {
		Groups []struct {
			ID    string `toml:"id"`
			Title string `toml:"title"`
		} `toml:"group"`
		Pages []struct {
			ID       string       `toml:"id"`
			Title    string       `toml:"title"`
			Mode     string       `toml:"mode"`
			Sections []rawSection `toml:"section"`
		} `toml:"page"`
		Sections []rawSection `toml:"section"`
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
		if _, ok := m.Group.(string); ok {
			legacy = true
		}
	}
	if len(data.Pages) > 0 {
		c := Config{Pages: make([]Page, 0, len(data.Pages))}
		for _, p := range data.Pages {
			secs, err := decodeRawSections(p.Sections, false)
			if err != nil {
				return Default(), err
			}
			c.Pages = append(c.Pages, Page{
				ID:       p.ID,
				Title:    p.Title,
				Mode:     p.Mode,
				Sections: secs,
			})
		}
		c.Sections = c.HomePage().Sections
		return c, nil
	}

	secs, err := decodeRawSections(data.Sections, legacy)
	if err != nil {
		return Default(), err
	}
	c := Config{
		Pages: []Page{
			{ID: "home", Title: "Home", Sections: secs},
		},
		Sections: secs,
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
	if strings.HasPrefix(q, "page:") {
		arg = strings.TrimPrefix(q, "page:")
		return "page", arg, nil
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
	if typ == "page" {
		if query == "home" || query == "" {
			return base
		}
		return base + "/page/" + url.PathEscape(query)
	}
	if typ == "search" {
		return base + "/gallery?query=" + url.QueryEscape(query)
	}
	kind, arg, e := ResolvePreset(query)
	if e != nil {
		return base + "/gallery"
	}
	switch kind {
	case "page":
		if arg == "home" || arg == "" {
			return base
		}
		return base + "/page/" + url.PathEscape(arg)
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
