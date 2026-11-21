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
	Title          string   `toml:"title" json:"title"`
	Type           string   `toml:"type,omitempty" json:"type,omitempty"`
	Query          string   `toml:"query,omitempty" json:"query,omitempty"`
	Categories     []string `toml:"categories,omitempty" json:"categories,omitempty"`
	Rating         int      `toml:"rating,omitempty" json:"rating,omitempty"`
	Expunged       bool     `toml:"expunged,omitempty" json:"expunged,omitempty"`
	DisableFilters []string `toml:"disable_filters,omitempty" json:"disable_filters,omitempty"`
}

type FacetSet struct {
	ID    string           `toml:"id" json:"id"`
	Title string           `toml:"title" json:"title"`
	Items []NavigationItem `toml:"items" json:"items"`
}

type Section struct {
	Group    bool             `toml:"group" json:"group"`
	Kind     string           `toml:"kind" json:"kind"`
	Title    string           `toml:"title" json:"title"`
	Type     string           `toml:"type" json:"type"`
	Query    string           `toml:"query" json:"query"`
	FacetSet string           `toml:"facet_set,omitempty" json:"facet_set,omitempty"`
	Count    *int             `toml:"count" json:"count"`
	Items    []NavigationItem `toml:"items,omitempty" json:"items,omitempty"`
}

type Page struct {
	ID       string    `toml:"id" json:"id"`
	Title    string    `toml:"title" json:"title"`
	Mode     string    `toml:"mode,omitempty" json:"mode,omitempty"`
	Sections []Section `toml:"section" json:"sections"`
}

type Config struct {
	Navigation   []Section        `toml:"navigation,omitempty" json:"navigation,omitempty"`
	SearchFacets []NavigationItem `toml:"search_facet,omitempty" json:"search_facets,omitempty"`
	FacetSets    []FacetSet       `toml:"facet_set,omitempty" json:"facet_sets,omitempty"`
	Pages        []Page           `toml:"page" json:"pages"`
	Sections     []Section        `toml:"section,omitempty" json:"sections,omitempty"`
}

func DefaultSearchFacets() []NavigationItem {
	return []NavigationItem{
		{Title: "Doujinshi", Categories: []string{"Doujinshi"}},
		{Title: "Manga", Categories: []string{"Manga"}},
		{Title: "Artist CG", Categories: []string{"Artist CG"}},
		{Title: "Game CG", Categories: []string{"Game CG"}},
		{Title: "Western", Categories: []string{"Western"}},
		{Title: "Non-H", Categories: []string{"Non-H"}},
		{Title: "Image Set", Categories: []string{"Image Set"}},
		{Title: "Cosplay", Categories: []string{"Cosplay"}},
		{Title: "Asian Porn", Categories: []string{"Asian Porn"}},
		{Title: "Misc", Categories: []string{"Misc"}},
	}
}

func (c Config) GetSearchFacets() []NavigationItem {
	if len(c.SearchFacets) > 0 {
		return c.SearchFacets
	}
	if len(c.FacetSets) > 0 && len(c.FacetSets[0].Items) > 0 {
		return c.FacetSets[0].Items
	}
	return DefaultSearchFacets()
}

func (c Config) GetFacetSet(id string) *FacetSet {
	for i := range c.FacetSets {
		if c.FacetSets[i].ID == id {
			return &c.FacetSets[i]
		}
	}
	return nil
}

func Default() Config {
	topNav := []Section{
		{Kind: "navigation", Type: "page", Query: "home"},
		{Kind: "navigation", Title: "Watched", Type: "preset", Query: "watched"},
		{Kind: "navigation", Title: "Favorites", Type: "preset", Query: "favorites"},
		{Kind: "navigation", Title: "Toplists", Type: "page", Query: "toplists"},
		{Kind: "navigation", Title: "Archives", Type: "preset", Query: "archives"},
	}

	homePage := Page{
		ID:    "home",
		Title: "Home",
		Mode:  "groups",
		Sections: []Section{
			{Group: true, Kind: "publication", Title: "Popular", Type: "preset", Query: "popular", Count: intp(20)},
			{Group: true, Kind: "publication", Title: "Latest", Type: "preset", Query: "latest", Count: intp(20)},
			{Group: true, Kind: "publication", Title: "Recently Read", Type: "preset", Query: "history", Count: intp(20)},
		},
	}

	toplistsPage := Page{
		ID:    "toplists",
		Title: "Toplists",
		Mode:  "groups",
		Sections: []Section{
			{Group: true, Kind: "publication", Title: "Yesterday", Type: "preset", Query: "toplist:yesterday", Count: intp(20)},
			{Group: true, Kind: "publication", Title: "Past Month", Type: "preset", Query: "toplist:month", Count: intp(20)},
			{Group: true, Kind: "publication", Title: "Past Year", Type: "preset", Query: "toplist:year", Count: intp(20)},
			{Group: true, Kind: "publication", Title: "All Time", Type: "preset", Query: "toplist:alltime", Count: intp(20)},
		},
	}

	return Config{
		Navigation:   topNav,
		SearchFacets: DefaultSearchFacets(),
		Pages:        []Page{homePage, toplistsPage},
		Sections:     homePage.Sections,
	}
}
func intp(v int) *int { return &v }

func IsBuiltinPageID(id string) bool {
	return id == "watched" || id == "favorites" || id == "archives"
}

func (c Config) GlobalNavigation() []Section {
	if len(c.Navigation) > 0 {
		return c.Navigation
	}
	for _, p := range c.Pages {
		var nav []Section
		for _, s := range p.Sections {
			if s.Kind == "navigation" && !s.Group {
				nav = append(nav, s)
			}
		}
		if len(nav) > 0 {
			return nav
		}
	}
	for _, s := range c.Sections {
		if s.Kind == "navigation" && !s.Group {
			return []Section{s}
		}
	}
	return nil
}

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
	switch id {
	case "watched":
		return Page{
			ID:    "watched",
			Title: "Watched",
			Mode:  "publications",
			Sections: []Section{
				{Group: false, Kind: "publication", Title: "Watched", Type: "preset", Query: "watched", Count: intp(20)},
			},
		}, true
	case "favorites":
		return Page{
			ID:    "favorites",
			Title: "Favorites",
			Mode:  "publications",
			Sections: []Section{
				{Group: false, Kind: "publication", Title: "Favorites", Type: "preset", Query: "favorites", Count: intp(20)},
			},
		}, true
	case "toplists":
		return Page{
			ID:    "toplists",
			Title: "Toplists",
			Mode:  "groups",
			Sections: []Section{
				{Group: true, Kind: "publication", Title: "Yesterday", Type: "preset", Query: "toplist:yesterday", Count: intp(20)},
				{Group: true, Kind: "publication", Title: "Past Month", Type: "preset", Query: "toplist:month", Count: intp(20)},
				{Group: true, Kind: "publication", Title: "Past Year", Type: "preset", Query: "toplist:year", Count: intp(20)},
				{Group: true, Kind: "publication", Title: "All Time", Type: "preset", Query: "toplist:alltime", Count: intp(20)},
			},
		}, true
	case "archives":
		return Page{
			ID:    "archives",
			Title: "Archives",
			Mode:  "publications",
			Sections: []Section{
				{Group: false, Kind: "publication", Title: "Archives", Type: "preset", Query: "archives", Count: intp(20)},
			},
		}, true
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
				if it.Type != "preset" && it.Type != "search" && it.Type != "page" && it.Type != "subscription" {
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
				} else if it.Type == "subscription" {
					if it.Query == "" {
						return fmt.Errorf("navigation subscription id cannot be empty")
					}
				} else if it.Query == "" {
					return fmt.Errorf("navigation item search query cannot be empty")
				}
			}
		} else {
			if s.Type != "preset" && s.Type != "search" && s.Type != "page" && s.Type != "subscription" {
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
			} else if s.Type == "subscription" {
				if s.Query == "" {
					return fmt.Errorf("navigation subscription id cannot be empty")
				}
			} else if s.Query == "" {
				return fmt.Errorf("search query cannot be empty")
			}
		}
	} else if s.Kind == "facets" {
		if s.FacetSet != "" {
			if !slugRE.MatchString(s.FacetSet) {
				return fmt.Errorf("invalid facet_set id %q", s.FacetSet)
			}
		} else if len(s.Items) > 0 {
			for _, it := range s.Items {
				if it.Title == "" {
					return fmt.Errorf("facet item title cannot be empty")
				}
				if it.Rating < 0 || it.Rating > 5 {
					return fmt.Errorf("invalid facet item rating %d (expected 0-5)", it.Rating)
				}
			}
		} else {
			if s.Type != "" && s.Type != "preset" && s.Type != "search" {
				return fmt.Errorf("invalid section type %q for facets", s.Type)
			}
			if s.Type == "preset" {
				kind, _, _ := ResolvePreset(s.Query)
				if kind != "search" && kind != "toplist" && kind != "favorites" && kind != "watched" && kind != "archives" && kind != "history" {
					return fmt.Errorf("preset has no facet provider: %s", s.Query)
				}
			}
		}
	} else {
		// publication
		if s.Type != "preset" && s.Type != "search" && s.Type != "subscription" {
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
	if s.Kind == "facets" && s.Type == "preset" && s.FacetSet == "" && len(s.Items) == 0 {
		kind, _, _ := ResolvePreset(s.Query)
		if kind != "search" && kind != "toplist" && kind != "favorites" && kind != "watched" && kind != "archives" && kind != "history" {
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
		if s.Kind == "facets" {
			if rootPublication == nil {
				return fmt.Errorf("page %q: facets require an ungrouped publication section", p.ID)
			}
			if s.FacetSet == "" && len(s.Items) == 0 && (s.Type != "" || s.Query != "") && (rootPublication.Type != s.Type || rootPublication.Query != s.Query) {
				return fmt.Errorf("page %q: facet source must match an ungrouped publication section", p.ID)
			}
		}
	}
	return nil
}

func Validate(c Config) error {
	for _, it := range c.SearchFacets {
		if strings.TrimSpace(it.Title) == "" {
			return fmt.Errorf("search facet item title cannot be empty")
		}
		if it.Rating < 0 || it.Rating > 5 {
			return fmt.Errorf("search facet %q: invalid item rating %d (expected 0-5)", it.Title, it.Rating)
		}
	}
	seenFS := make(map[string]bool)
	for _, fs := range c.FacetSets {
		id := strings.TrimSpace(fs.ID)
		if id == "" || !slugRE.MatchString(id) {
			return fmt.Errorf("invalid facet set id %q", fs.ID)
		}
		if seenFS[id] {
			return fmt.Errorf("duplicate facet set id %q", id)
		}
		seenFS[id] = true
		for _, it := range fs.Items {
			if strings.TrimSpace(it.Title) == "" {
				return fmt.Errorf("facet set %q: item title cannot be empty", id)
			}
			if it.Rating < 0 || it.Rating > 5 {
				return fmt.Errorf("facet set %q: invalid item rating %d", id, it.Rating)
			}
		}
	}
	for _, s := range c.Navigation {
		if s.Kind != "navigation" {
			return fmt.Errorf("global navigation entry must have kind 'navigation'")
		}
		if s.Group {
			return fmt.Errorf("global navigation entry cannot be grouped")
		}
		if err := ValidateSection(s); err != nil {
			return fmt.Errorf("global navigation: %w", err)
		}
	}
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
		for _, s := range p.Sections {
			if s.Kind == "facets" && s.FacetSet != "" {
				if !seenFS[s.FacetSet] {
					return fmt.Errorf("page %q references unknown facet set %q", p.ID, s.FacetSet)
				}
			}
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
	if len(toSave.Navigation) == 0 {
		toSave.Navigation = toSave.GlobalNavigation()
	}
	// Strip redundant ungrouped navigation and built-in preset pages on save
	var filteredPages []Page
	for i := range toSave.Pages {
		if IsBuiltinPageID(toSave.Pages[i].ID) {
			continue
		}
		var kept []Section
		for _, s := range toSave.Pages[i].Sections {
			if s.Kind == "navigation" && !s.Group {
				continue
			}
			kept = append(kept, s)
		}
		pCopy := toSave.Pages[i]
		pCopy.Sections = kept
		filteredPages = append(filteredPages, pCopy)
	}
	toSave.Pages = filteredPages
	// Do not write legacy top-level sections if Pages are present
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
	if len(c.Navigation) > 0 {
		c.Navigation = normalizeSections(c.Navigation)
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
	navCounts := make([]*int, len(raw.Navigation))
	for i, s := range raw.Navigation {
		if s.Count != nil {
			n := *s.Count
			navCounts[i] = &n
		}
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
	for i := range c.Navigation {
		if i < len(navCounts) {
			c.Navigation[i].Count = navCounts[i]
		}
	}
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
	Group    any              `toml:"group"`
	Kind     any              `toml:"kind"`
	Title    any              `toml:"title"`
	Type     any              `toml:"type"`
	Query    any              `toml:"query"`
	FacetSet any              `toml:"facet_set"`
	Count    any              `toml:"count"`
	Items    []NavigationItem `toml:"items"`
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
		if m.FacetSet != nil {
			if _, ok := m.FacetSet.(string); !ok {
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
		if v, ok := m.FacetSet.(string); ok {
			s.FacetSet = v
		}
		if v, ok := m.Count.(int64); ok {
			n := int(v)
			s.Count = &n
		}
		if len(m.Items) > 0 {
			s.Items = append(s.Items, m.Items...)
		}
		// Migrate legacy preset:toplists to page:toplists
		if s.Type == "preset" && s.Query == "toplists" {
			s.Type = "page"
		}
		for i := range s.Items {
			if s.Items[i].Type == "preset" && s.Items[i].Query == "toplists" {
				s.Items[i].Type = "page"
			}
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
		Navigation   []rawSection     `toml:"navigation"`
		SearchFacets []NavigationItem `toml:"search_facet"`
		FacetSets    []FacetSet       `toml:"facet_set"`
		Pages        []struct {
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
	var nav []Section
	if len(data.Navigation) > 0 {
		var err error
		nav, err = decodeRawSections(data.Navigation, false)
		if err != nil {
			return Default(), err
		}
	}
	searchFacets := data.SearchFacets
	if len(searchFacets) == 0 && len(data.FacetSets) > 0 {
		searchFacets = data.FacetSets[0].Items
	}
	legacy := len(data.Groups) > 0
	for _, m := range data.Sections {
		if _, ok := m.Group.(string); ok {
			legacy = true
		}
	}
	if len(data.Pages) > 0 {
		c := Config{Navigation: nav, SearchFacets: searchFacets, FacetSets: data.FacetSets, Pages: make([]Page, 0, len(data.Pages))}
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
		Navigation:   nav,
		SearchFacets: searchFacets,
		FacetSets:    data.FacetSets,
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
	case "popular", "watched", "favorites", "archives", "history":
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
	if typ == "subscription" {
		return base + "/subscription/" + url.PathEscape(query)
	}
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
	case "toplists":
		return base + "/page/toplists"
	case "archives":
		return base + "/archives"
	case "history":
		return base + "/history"
	}
	return base + "/gallery"
}

func AuthRequired(s Section) bool {
	return s.Query == "watched" || s.Query == "favorites" || strings.HasPrefix(s.Query, "favorites:")
}
