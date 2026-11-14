package app

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"pandaopds/internal/eh"
	"pandaopds/internal/feed"
	"pandaopds/internal/home"
	"pandaopds/internal/progression"
)

var cursorRE = regexp.MustCompile(`^\d+(?:-\d+)*$`)
var periods = map[string]string{"yesterday": "Yesterday", "month": "Past Month", "year": "Past Year", "alltime": "All Time"}

const homepagePreviewDeadline = 12 * time.Second

func observeProgressionVersions(store *progression.Store, gid int, token string, activeGID int, activeToken string, versions []eh.GalleryVersion) {
	_ = store.Observe(gid, token, activeGID, activeToken)
	for _, v := range versions {
		if v.GID > 0 && v.Token != "" {
			_ = store.Observe(v.GID, v.Token, activeGID, activeToken)
		}
	}
}

type homePreviewKey struct {
	typeName string
	query    string
}

type homePreviewResult struct {
	info eh.GalleryPageInfo
	err  error
}

type homePreviewDone struct {
	key    homePreviewKey
	result homePreviewResult
}

func quote(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }

func favoriteFacetPath(route, favcat string) string {
	values := url.Values{"query": {"favorites"}}
	if favcat != "" {
		values.Set("favcat", favcat)
	}
	return route + "?" + values.Encode()
}

func favoriteFacetTitle(names map[int]string, id int) string {
	if name := strings.TrimSpace(names[id]); name != "" {
		return name
	}
	return fmt.Sprintf("Favorites %d", id)
}

func (s *Server) favoriteFacetNames(ctx context.Context, current map[int]string) map[int]string {
	names := make(map[int]string, len(current)+10)
	for id, name := range current {
		names[id] = name
	}
	missing := false
	for id := 0; id < 10; id++ {
		if strings.TrimSpace(names[id]) == "" {
			missing = true
			break
		}
	}
	if !missing {
		return names
	}
	upstream, err := s.Service.FavoriteCategories(ctx)
	if err != nil {
		return names
	}
	for id := 0; id < 10; id++ {
		if strings.TrimSpace(names[id]) == "" {
			if name := strings.TrimSpace(upstream[id]); name != "" {
				names[id] = name
			}
		}
	}
	return names
}

func favoriteAtomFacets(builder feed.Builder, route, selected string, names map[int]string) []feed.AtomFacet {
	facets := []feed.AtomFacet{{Title: "All", Href: builder.Href(favoriteFacetPath(route, "")), Group: "Favorite category", Active: selected == ""}}
	for id := 0; id < 10; id++ {
		value := strconv.Itoa(id)
		facets = append(facets, feed.AtomFacet{Title: favoriteFacetTitle(names, id), Href: builder.Href(favoriteFacetPath(route, value)), Group: "Favorite category", Active: selected == value})
	}
	return facets
}

func favoriteJSONFacets(builder feed.Builder, route, selected string, names map[int]string) []map[string]any {
	return []map[string]any{{"metadata": map[string]string{"title": "Favorite category"}, "links": favoriteCategoryLinks(builder, route, selected, names)}}
}

func favoriteCategoryLinks(builder feed.Builder, route, selected string, names map[int]string) []map[string]any {
	links := []map[string]any{}
	for id := -1; id < 10; id++ {
		favcat := ""
		title := "All"
		if id >= 0 {
			favcat = strconv.Itoa(id)
			title = favoriteFacetTitle(names, id)
		}
		link := map[string]any{"href": builder.Href(favoriteFacetPath(route, favcat)), "title": title}
		if selected == favcat {
			link["rel"] = "self"
		}
		links = append(links, link)
	}
	return links
}

func (s *Server) builder() feed.Builder {
	var translator feed.SubjectTranslator
	if s.Service != nil {
		translator = s.Service.TagTranslator()
	}
	return feed.Builder{Settings: s.Settings, Translator: translator}
}
func (s *Server) redirectOPDS(target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		location := s.builder().Href(target)
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			location += "?" + r.URL.RawQuery
		}
		w.Header().Set("Location", location)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}
}
func (s *Server) rootV12(w http.ResponseWriter, r *http.Request) {
	b := s.builder()
	nav := [][3]string{{"Latest", "/opds/v1.2/gallery", "Latest"}}
	if s.Settings.HasIPB() {
		nav = append(nav, [3]string{"Watched", "/opds/v1.2/gallery?query=watched", "Watched"}, [3]string{"Favorites", "/opds/v1.2/gallery?query=favorites", "Favorites"})
	}
	if s.Store.ReadyCount() > 0 {
		nav = append(nav, [3]string{"Archives", "/opds/v1.2/archives", "Archives"})
	}
	nav = append(nav, [3]string{"Popular", "/opds/v1.2/gallery?query=popular", "Popular"}, [3]string{"Toplist", "/opds/v1.2/toplist?period=yesterday", "Toplist"}, [3]string{"Search", "/opds/v1.2/search.xml", "Search E-Hentai galleries"})
	writeBytes(w, 200, feed.MIMEAtomNav, map[string]string{"Cache-Control": "public, max-age=300"}, []byte(b.RootAtom(nav)))
}
func (s *Server) searchXML12(w http.ResponseWriter, r *http.Request) {
	writeBytes(w, 200, feed.MIMEOpenSearch, map[string]string{"Cache-Control": "public, max-age=3600"}, []byte(s.builder().OpenSearch("/opds/v1.2/gallery")))
}
func (s *Server) gallery12(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := q.Get("query")
	next := q.Get("next")
	favcat := q.Get("favcat")
	if query == "favorites" && favcat != "" {
		id, err := strconv.Atoi(favcat)
		if err != nil || id < 0 || id > 9 {
			writeJSON(w, 400, map[string]any{"detail": "unknown favorites folder (expected favcat 0-9)"})
			return
		}
	}
	if next != "" && !cursorRE.MatchString(next) {
		writeJSON(w, 400, map[string]any{"detail": "invalid next cursor (expected digits or gid-favtime)"})
		return
	}
	var info eh.GalleryPageInfo
	var e error
	ctx := r.Context()
	switch query {
	case "popular":
		info, e = s.Service.Popular(ctx, next)
	case "watched":
		info, e = s.Service.Watched(ctx, next)
	case "favorites":
		info, e = s.Service.Favorites(ctx, next, favcat)
	default:
		info, e = s.Service.Search(ctx, query, next, nil)
	}
	if e != nil {
		writeError(w, e)
		return
	}
	title := "Latest"
	switch query {
	case "popular":
		title = "Popular"
	case "watched":
		title = "Watched"
	case "favorites":
		title = "Favorites"
	case "":
	default:
		title = "Search"
	}
	title = "E-Hentai: " + title
	nextHref := ""
	if info.NextGID != "" {
		nextHref = s.builder().Href("/opds/v1.2/gallery?next=" + info.NextGID)
		if query != "" {
			nextHref += "&query=" + quote(query)
		}
		if query == "favorites" && favcat != "" {
			nextHref += "&favcat=" + quote(favcat)
		}
	}
	var facets []feed.AtomFacet
	if query == "favorites" {
		names := s.favoriteFacetNames(ctx, info.FavcatMap)
		facets = favoriteAtomFacets(s.builder(), "/opds/v1.2/gallery", favcat, names)
	}
	writeBytes(w, 200, feed.MIMEAtomAcq, map[string]string{"Cache-Control": "public, max-age=300"}, []byte(s.builder().AtomList(info.Galleries, title, query, query, nextHref, facets)))
}
func (s *Server) toplist12(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "yesterday"
	}
	if _, ok := periods[period]; !ok {
		writeJSON(w, 400, map[string]any{"detail": fmt.Sprintf("unknown period %q (expected [alltime month yesterday year])", period)})
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	info, e := s.Service.Toplist(r.Context(), period, page)
	if e != nil {
		writeError(w, e)
		return
	}
	title := "E-Hentai: Toplist " + periods[period]
	next := ""
	if info.NextPage != nil {
		next = s.builder().Href(fmt.Sprintf("/opds/v1.2/toplist?period=%s&page=%d", period, *info.NextPage))
	}
	facets := []feed.AtomFacet{}
	for p, label := range periods {
		facets = append(facets, feed.AtomFacet{Title: label, Href: s.builder().Href("/opds/v1.2/toplist?period=" + p), Group: "period", Active: p == period})
	}
	writeBytes(w, 200, feed.MIMEAtomAcq, map[string]string{"Cache-Control": "public, max-age=300"}, []byte(s.builder().AtomList(info.Galleries, title, "toplist:"+period, "", next, facets)))
}
func (s *Server) archives12(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	items := s.Service.ArchivedGalleries(page)
	next := ""
	if page*100 < s.Store.ReadyCount() {
		next = s.builder().Href(fmt.Sprintf("/opds/v1.2/archives?page=%d", page+1))
	}
	writeBytes(w, 200, feed.MIMEAtomAcq, map[string]string{"Cache-Control": "public, max-age=300"}, []byte(s.builder().AtomList(items, "E-Hentai: Archives", "archives", "", next, nil)))
}
func (s *Server) chapter12(w http.ResponseWriter, r *http.Request) {
	gid, token, e := pathID(r)
	if e != nil {
		writeJSON(w, 422, map[string]any{"detail": e.Error()})
		return
	}
	d, e := s.Service.GetDetail(r.Context(), gid, token)
	if e != nil {
		writeError(w, e)
		return
	}
	title, authors := feed.ParseDetailTitle(d.Title, d.TitleJPN)
	author := ""
	if len(authors) > 0 {
		author = strings.Join(authors, ", ")
	}
	activeGID, activeToken := d.ActiveGID, d.ActiveToken
	if activeGID <= 0 || activeToken == "" {
		activeGID, activeToken = gid, token
	}
	item := eh.GalleryListItem{GID: gid, Token: token, ActiveGID: activeGID, ActiveToken: activeToken, Title: title, Category: d.Category, PublishTime: d.PublishTime, PageCount: &d.ImageCount}
	if s.Progression != nil {
		observeProgressionVersions(s.Progression, gid, token, activeGID, activeToken, d.NewerVersions)
	}
	writeBytes(w, 200, feed.MIMEAtomAcq, map[string]string{"Cache-Control": "public, max-age=300"}, []byte(s.builder().AtomChapterVersion(gid, token, item, title, author)))
}

func (s *Server) searchXML20(w http.ResponseWriter, r *http.Request) {
	writeBytes(w, 200, feed.MIMEOpenSearch, map[string]string{"Cache-Control": "public, max-age=3600"}, []byte(s.builder().OpenSearch("/opds/v2.0/gallery")))
}
func (s *Server) gallery20(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query, next, category, favcat := q.Get("query"), q.Get("next"), q.Get("category"), q.Get("favcat")
	if next != "" && !cursorRE.MatchString(next) {
		writeJSON(w, 400, map[string]any{"detail": "invalid next cursor (expected digits or gid-favtime)"})
		return
	}
	extra := url.Values{}
	if category != "" {
		for _, f := range s.Settings.Facets {
			if strings.EqualFold(category, f.Name) {
				extra.Set("f_cats", strconv.Itoa(f.Mask))
				break
			}
		}
	}
	var info eh.GalleryPageInfo
	var e error
	ctx := r.Context()
	switch {
	case query == "popular":
		info, e = s.Service.Popular(ctx, next)
	case query == "watched":
		info, e = s.Service.Watched(ctx, next)
	case query == "favorites" || strings.HasPrefix(query, "favorites:"):
		cat := favcat
		if strings.HasPrefix(query, "favorites:") {
			legacy := strings.TrimPrefix(query, "favorites:")
			if !regexp.MustCompile(`^\d+$`).MatchString(legacy) {
				writeJSON(w, 400, map[string]any{"detail": fmt.Sprintf("unknown favorites folder %q (expected favorites or favorites:N)", legacy)})
				return
			}
			if cat == "" {
				cat = legacy
			}
		}
		if cat != "" {
			id, err := strconv.Atoi(cat)
			if err != nil || id < 0 || id > 9 {
				writeJSON(w, 400, map[string]any{"detail": "unknown favorites folder (expected favcat 0-9)"})
				return
			}
		}
		info, e = s.Service.Favorites(ctx, next, cat)
	default:
		info, e = s.Service.Search(ctx, query, next, extra)
	}
	if e != nil {
		writeError(w, e)
		return
	}
	pubs := []map[string]any{}
	for _, item := range info.Galleries {
		pubs = append(pubs, s.builder().Publication(item, false))
	}
	name := query
	if strings.HasPrefix(query, "favorites:") {
		name = "Favorites " + strings.TrimPrefix(query, "favorites:")
	}
	switch query {
	case "":
		name = "Latest"
	case "popular", "watched", "favorites":
		name = strings.ToUpper(query[:1]) + query[1:]
	}
	if category != "" {
		name += " — " + category
	}
	title := "E-Hentai: " + name
	nextHref := ""
	if info.NextGID != "" {
		nextHref = s.builder().Href("/opds/v2.0/gallery?next=" + info.NextGID)
		params := []string{}
		if query == "favorites" || strings.HasPrefix(query, "favorites:") {
			params = append(params, "query=favorites")
			selected := favcat
			if selected == "" && strings.HasPrefix(query, "favorites:") {
				selected = strings.TrimPrefix(query, "favorites:")
			}
			if selected != "" {
				params = append(params, "favcat="+quote(selected))
			}
		} else if query != "" {
			params = append(params, "query="+quote(query))
		}
		if favcat != "" && query != "favorites" && !strings.HasPrefix(query, "favorites:") {
			params = append(params, "favcat="+quote(favcat))
		}
		if category != "" {
			params = append(params, "category="+quote(category))
		}
		if len(params) > 0 {
			nextHref += "&" + strings.Join(params, "&")
		}
	}
	var facets []map[string]any
	if query == "favorites" || strings.HasPrefix(query, "favorites:") {
		selected := favcat
		if selected == "" && strings.HasPrefix(query, "favorites:") {
			selected = strings.TrimPrefix(query, "favorites:")
		}
		names := s.favoriteFacetNames(ctx, info.FavcatMap)
		facets = favoriteJSONFacets(s.builder(), "/opds/v2.0/gallery", selected, names)
	} else if query != "popular" && query != "watched" {
		facets = s.builder().CategoryFacets()
	}
	self := "/opds/v2.0/gallery"
	if query == "favorites" || strings.HasPrefix(query, "favorites:") {
		selected := favcat
		if selected == "" && strings.HasPrefix(query, "favorites:") {
			selected = strings.TrimPrefix(query, "favorites:")
		}
		self += "?query=favorites"
		if selected != "" {
			self += "&favcat=" + quote(selected)
		}
	} else if query != "" {
		self += "?query=" + quote(query)
		if favcat != "" {
			self += "&favcat=" + quote(favcat)
		}
		if category != "" {
			self += "&category=" + quote(category)
		}
	} else if category != "" {
		self += "?category=" + quote(category)
	}
	doc := s.builder().Acquisition(title, "urn:ehentai:gallery-list:"+firstNonEmpty(query, "latest"), self, pubs, nextHref, facets)
	writeBytes(w, 200, feed.MIMEOPDSAcq, map[string]string{"Cache-Control": "public, max-age=300"}, s.builder().JSON(doc))
}
func (s *Server) toplist20(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "yesterday"
	}
	if _, ok := periods[period]; !ok {
		writeJSON(w, 400, map[string]any{"detail": fmt.Sprintf("unknown period %q (expected [alltime month yesterday year])", period)})
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	info, e := s.Service.Toplist(r.Context(), period, page)
	if e != nil {
		writeError(w, e)
		return
	}
	pubs := []map[string]any{}
	for _, item := range info.Galleries {
		pubs = append(pubs, s.builder().Publication(item, false))
	}
	next := ""
	if info.NextPage != nil {
		next = s.builder().Href(fmt.Sprintf("/opds/v2.0/toplist?period=%s&page=%d", period, *info.NextPage))
	}
	links := []map[string]any{}
	for p, label := range periods {
		link := map[string]any{"href": s.builder().Href("/opds/v2.0/toplist?period=" + p), "title": label}
		if p == period {
			link["active"] = true
		}
		links = append(links, link)
	}
	facets := []map[string]any{{"metadata": map[string]string{"title": "Period"}, "links": links}}
	title := "E-Hentai: Toplist " + periods[period]
	doc := s.builder().Acquisition(title, "urn:ehentai:toplist:"+period, "/opds/v2.0/toplist?period="+period, pubs, next, facets)
	writeBytes(w, 200, feed.MIMEOPDSAcq, map[string]string{"Cache-Control": "public, max-age=300"}, s.builder().JSON(doc))
}
func (s *Server) archives20(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	items := s.Service.ArchivedGalleries(page)
	pubs := []map[string]any{}
	for _, item := range items {
		pubs = append(pubs, s.builder().Publication(item, false))
	}
	next := ""
	if page*100 < s.Store.ReadyCount() {
		next = s.builder().Href(fmt.Sprintf("/opds/v2.0/archives?page=%d", page+1))
	}
	doc := s.builder().Acquisition("E-Hentai: Archives", "urn:ehentai:archives", "/opds/v2.0/archives", pubs, next, nil)
	writeBytes(w, 200, feed.MIMEOPDSAcq, map[string]string{"Cache-Control": "public, max-age=300"}, s.builder().JSON(doc))
}
func (s *Server) rootV20(w http.ResponseWriter, r *http.Request) {
	cfg, e := home.Load(s.Settings.HomeConfigPath)
	if e != nil && s.Logger != nil {
		s.Logger.Printf("home config load failed, using defaults: %v", e)
	}
	sections := []home.Section{}
	for _, sec := range cfg.Sections {
		if home.AuthRequired(sec) && !s.Settings.HasIPB() {
			continue
		}
		if sec.Type == "preset" && sec.Query == "archives" && s.Store.ReadyCount() == 0 {
			continue
		}
		sections = append(sections, sec)
	}
	rootNav := []map[string]any{}
	outGroups := []map[string]any{}
	previews, previewTimedOut := s.fetchHomePreviews(r.Context(), sections)
	rootPubs := []map[string]any{}
	var rootSource homePreviewKey
	for _, sec := range sections {
		title := firstNonEmpty(sec.Title, sec.Query)
		href := s.builder().Href(home.BuildHref(sec.Type, sec.Query))
		if sec.Kind == "navigation" {
			link := map[string]any{"title": title, "href": href, "rel": "subsection", "type": feed.MIMEOPDSAcq}
			if sec.Group {
				outGroups = append(outGroups, map[string]any{"metadata": map[string]any{"title": title, "modified": isoNow()}, "navigation": []map[string]any{link}})
			} else {
				rootNav = append(rootNav, link)
			}
			continue
		}
		if sec.Kind != "publication" {
			continue
		}
		count := previewCount(sec)
		if count <= 0 {
			continue
		}
		result, ok := previews[homePreviewKey{sec.Type, sec.Query}]
		if !ok || result.err != nil {
			if s.Logger != nil {
				err := result.err
				if !ok {
					err = context.DeadlineExceeded
				}
				s.Logger.Printf("home section %q: %v", title, err)
			}
			continue
		}
		if count > len(result.info.Galleries) {
			count = len(result.info.Galleries)
		}
		pubs := []map[string]any{}
		for _, item := range result.info.Galleries[:count] {
			pubs = append(pubs, s.builder().Publication(item, false))
		}
		if len(pubs) == 0 {
			continue
		}
		if sec.Group {
			g := map[string]any{"metadata": map[string]any{"title": title, "modified": isoNow()}, "publications": pubs, "links": []map[string]any{{"rel": "self", "href": href, "type": feed.MIMEOPDSAcq, "title": title}}}
			outGroups = append(outGroups, g)
		} else {
			rootPubs = pubs
			rootSource = homePreviewKey{sec.Type, sec.Query}
		}
	}

	rootFacetGroups := []map[string]any{}
	for _, sec := range sections {
		if sec.Kind != "facets" || len(rootPubs) == 0 || rootSource != (homePreviewKey{sec.Type, sec.Query}) {
			continue
		}
		links := s.homeFacetLinks(sec, previews[rootSource].info)
		if len(links) >= 2 {
			rootFacetGroups = append(rootFacetGroups, map[string]any{
				"metadata": map[string]string{"title": firstNonEmpty(sec.Title, sec.Query)},
				"links":    links,
			})
		}
	}

	if len(rootNav) == 0 && len(outGroups) == 0 && len(rootPubs) == 0 {
		rootNav = append(rootNav, map[string]any{"title": "Latest", "href": s.builder().Href(home.BuildHref("preset", "latest")), "rel": "subsection", "type": feed.MIMEOPDSAcq})
	}

	doc := s.builder().NavigationDocument(rootNav, outGroups)
	if len(rootPubs) > 0 {
		doc["publications"] = rootPubs
	}
	if len(rootFacetGroups) > 0 {
		doc["facets"] = rootFacetGroups
	}
	cacheControl := "public, max-age=300"
	if previewTimedOut {
		cacheControl = "no-store"
	} else if r.Header.Get("Cache-Control") == "no-cache" || r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		cacheControl = "no-cache, no-store"
	}
	writeBytes(w, 200, feed.MIMEOPDSNav, map[string]string{"Cache-Control": cacheControl}, s.builder().JSON(doc))
}
func (s *Server) homeFacetLinks(sec home.Section, info eh.GalleryPageInfo) []map[string]any {
	builder := s.builder()
	const galleryRoute = "/opds/v2.0/gallery"
	if sec.Type == "search" || sec.Query == "latest" {
		base := home.BuildHref(sec.Type, sec.Query)
		links := []map[string]any{builder.Link("self", builder.Href(base), feed.MIMEOPDSAcq, "All", nil)}
		for _, category := range s.Settings.Facets {
			params := url.Values{"category": {category.Name}}
			if sec.Type == "search" {
				params.Set("query", sec.Query)
			}
			links = append(links, map[string]any{"href": builder.Href(galleryRoute + "?" + params.Encode()), "type": feed.MIMEOPDSAcq, "title": category.Name})
		}
		return links
	}

	kind, selected, _ := home.ResolvePreset(sec.Query)
	if kind == "favorites" {
		return favoriteCategoryLinks(builder, galleryRoute, selected, info.FavcatMap)
	}
	links := []map[string]any{}
	for _, period := range []string{"yesterday", "month", "year", "alltime"} {
		link := map[string]any{"href": builder.Href("/opds/v2.0/toplist?period=" + period), "type": feed.MIMEOPDSAcq, "title": periods[period]}
		if period == selected {
			link["rel"] = "self"
		}
		links = append(links, link)
	}
	return links
}

func previewCount(sec home.Section) int {
	if sec.Count == nil {
		return 10
	}
	return *sec.Count
}

func (s *Server) fetchHomePreviews(parent context.Context, sections []home.Section) (map[homePreviewKey]homePreviewResult, bool) {
	keys := make([]homePreviewKey, 0, len(sections))
	seen := make(map[homePreviewKey]bool)
	for _, sec := range sections {
		if sec.Kind != "publication" || previewCount(sec) <= 0 {
			continue
		}
		key := homePreviewKey{sec.Type, sec.Query}
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	results := make(map[homePreviewKey]homePreviewResult, len(keys))
	if len(keys) == 0 {
		return results, false
	}
	if err := parent.Err(); err != nil {
		return results, err == context.DeadlineExceeded
	}
	ctx, cancel := context.WithTimeout(parent, homepagePreviewDeadline)
	defer cancel()
	completed := make(chan homePreviewDone, len(keys))
	for _, key := range keys {
		key := key
		go func() {
			result := homePreviewResult{}
			defer func() {
				if recovered := recover(); recovered != nil {
					result.err = fmt.Errorf("preview worker panic: %v", recovered)
					if s.Logger != nil {
						s.Logger.Printf("home preview %q: %v", key.query, result.err)
					}
				}
				completed <- homePreviewDone{key, result}
			}()
			if ctx.Err() != nil {
				result.err = ctx.Err()
				return
			}
			sec := home.Section{Type: key.typeName, Query: key.query}
			result.info, result.err = s.fetchHomeSection(ctx, sec)
		}()
	}
	pending := len(keys)
	for pending > 0 {
		select {
		case done := <-completed:
			results[done.key] = done.result
			pending--
		case <-ctx.Done():
			for {
				select {
				case done := <-completed:
					results[done.key] = done.result
				default:
					return results, ctx.Err() == context.DeadlineExceeded
				}
			}
		}
	}
	return results, ctx.Err() == context.DeadlineExceeded
}

func (s *Server) fetchHomeSection(ctx context.Context, sec home.Section) (eh.GalleryPageInfo, error) {
	if err := ctx.Err(); err != nil {
		return eh.GalleryPageInfo{}, err
	}
	if sec.Type == "search" {
		return s.Service.Search(ctx, sec.Query, "", nil)
	}
	kind, arg, e := home.ResolvePreset(sec.Query)
	if e != nil {
		return eh.GalleryPageInfo{}, e
	}
	switch kind {
	case "search":
		return s.Service.Search(ctx, arg, "", nil)
	case "popular":
		return s.Service.Popular(ctx, "")
	case "watched":
		return s.Service.Watched(ctx, "")
	case "favorites":
		return s.Service.Favorites(ctx, "", arg)
	case "archives":
		return eh.GalleryPageInfo{Galleries: s.Service.ArchivedGalleries(1)}, nil
	case "toplist":
		return s.Service.Toplist(ctx, arg, 1)
	}
	return eh.GalleryPageInfo{}, fmt.Errorf("unknown section")
}
func (s *Server) detail20(w http.ResponseWriter, r *http.Request) {
	gid, token, e := pathID(r)
	if e != nil {
		writeJSON(w, 422, map[string]any{"detail": e.Error()})
		return
	}
	pub, e := s.detailPublication(r, gid, token)
	if e != nil {
		writeError(w, e)
		return
	}
	md, _ := pub["metadata"].(map[string]any)
	title := fmt.Sprint(md["title"])
	doc := s.builder().Acquisition(title, fmt.Sprintf("urn:ehentai:gallery:%d:%s", gid, token), fmt.Sprintf("/opds/v2.0/gallery/%d/%s", gid, token), []map[string]any{pub}, "", nil)
	writeBytes(w, 200, feed.MIMEOPDSAcq, map[string]string{"Cache-Control": "public, max-age=300"}, s.builder().JSON(doc))
}
func (s *Server) publication20(w http.ResponseWriter, r *http.Request) {
	gid, token, e := pathID(r)
	if e != nil {
		writeJSON(w, 422, map[string]any{"detail": e.Error()})
		return
	}
	pub, e := s.detailPublication(r, gid, token)
	if e != nil {
		writeError(w, e)
		return
	}
	writeBytes(w, 200, feed.MIMEPublication, map[string]string{"Cache-Control": "public, max-age=300"}, s.builder().JSON(pub))
}

func (s *Server) manifest20(w http.ResponseWriter, r *http.Request) {
	gid, token, e := pathID(r)
	if e != nil {
		writeJSON(w, 422, map[string]any{"detail": e.Error()})
		return
	}
	pub, e := s.detailPublication(r, gid, token)
	if e != nil {
		writeError(w, e)
		return
	}
	reading, _ := pub["readingOrder"].([]map[string]any)
	if len(reading) == 0 {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": "gallery page count unavailable; cannot build a Divina manifest"})
		return
	}
	delete(pub, "context")
	pub["@context"] = feed.RWPMContext
	metadata := pub["metadata"].(map[string]any)
	metadata["conformsTo"] = []string{feed.DivinaProfile}
	writeBytes(w, http.StatusOK, feed.MIMEDivina, map[string]string{"Cache-Control": "public, max-age=300"}, s.builder().JSON(pub))
}

func (s *Server) detailPublication(r *http.Request, gid int, token string) (map[string]any, error) {
	d, e := s.Service.GetDetail(r.Context(), gid, token)
	if e != nil {
		return nil, e
	}
	title, authors := feed.ParseDetailTitle(d.Title, d.TitleJPN)
	count := d.ImageCount
	activeGID, activeToken := d.ActiveGID, d.ActiveToken
	if activeGID <= 0 || activeToken == "" {
		activeGID, activeToken = gid, token
	}
	if s.Progression != nil {
		observeProgressionVersions(s.Progression, gid, token, activeGID, activeToken, d.NewerVersions)
	}
	if activeGID == gid && activeToken == token && s.Store.IsReady(gid, token) {
		if n := s.Store.PageCount(gid, token); n > 0 {
			count = n
		}
	}
	modified := feed.FormatDate(d.PublishTime)
	extra := map[string]any{}
	if d.Rating > 0 {
		extra["x:rating"] = d.Rating
	}
	if d.TitleJPN != "" {
		extra["x:titleJpn"] = d.TitleJPN
	}
	if d.Uploader != "" {
		extra["x:uploader"] = d.Uploader
	}
	if d.FilesizeBytes != nil {
		extra["x:sizeBytes"] = *d.FilesizeBytes
	} else if d.FilesizeText != "" {
		if n := eh.ParseSizeText(d.FilesizeText); n > 0 {
			extra["x:sizeBytes"] = n
		}
	}
	if d.Expunged {
		extra["x:expunged"] = true
	}
	if note := newerVersionsNote(d.NewerVersions, s.builder().Href); note != "" {
		extra["x:note"] = note
	}
	item := eh.GalleryListItem{GID: gid, Token: token, ActiveGID: activeGID, ActiveToken: activeToken, Title: title, PageCount: &count, Language: d.Language, PublishTime: d.PublishTime, Tags: d.Tags, Category: d.Category}
	pub := s.builder().Publication(item, true)
	metadata := pub["metadata"].(map[string]any)
	metadata["title"] = title
	metadata["modified"] = modified
	if len(authors) > 0 {
		aa := []map[string]string{}
		for _, a := range authors {
			aa = append(aa, map[string]string{"name": a})
		}
		metadata["authors"] = aa
		metadata["author"] = aa
	}
	for k, v := range extra {
		metadata[k] = v
	}
	if s.Settings.HasIPB() {
		if s.Store.IsReady(gid, token) {
			d.Tags = s.Service.ApplyMyTagStyles(d.Tags)
		} else {
			d.Tags = eh.ApplyTagsWithStyles(d.Tags, s.Service.GetMyTags(r.Context()))
		}
	}
	metadata["subject"] = s.builder().Subjects(d.Tags, d.Category, true)
	if s.Settings.CommentsEnabled && len(d.Comments) > 0 {
		reviews := make([]map[string]any, 0, len(d.Comments))
		for _, comment := range d.Comments {
			item := map[string]any{"id": comment.ID, "username": comment.Username, "time": comment.Time}
			if comment.UserID != nil {
				item["userId"] = *comment.UserID
			}
			if comment.LastEditTime != "" {
				item["lastEditTime"] = comment.LastEditTime
			}
			if comment.ContentHTML != "" {
				item["content"] = s.rewriteCommentHTML(comment.ContentHTML)
			}
			reviews = append(reviews, item)
		}
		metadata["x:reviews"] = reviews
	}
	return pub, nil
}

func newerVersionsNote(versions []eh.GalleryVersion, href func(string) string) string {
	if len(versions) == 0 {
		return ""
	}
	chosen := versions[len(versions)-1]
	latest := ""
	for _, version := range versions {
		if version.AddedAt != "" && version.AddedAt > latest {
			chosen = version
			latest = version.AddedAt
		}
	}
	target := href(fmt.Sprintf("/opds/v2.0/gallery/%d/%s", chosen.GID, chosen.Token))
	return "There are newer versions of this gallery available:<br><a href=\"" + html.EscapeString(target) + "\">" + html.EscapeString(chosen.Title) + "</a>"
}

var commentGalleryHrefRE = regexp.MustCompile(`(?i)(href=["'])(https?://(?:e-hentai|exhentai)\.org/(?:g|mpv)/(\d+)/([0-9a-f]+)/[^"']*)(["'])`)
var commentImageTokenRE = regexp.MustCompile(`(?i)(src=["']|url\(["']?)(https://[^"')\s]+)`)

func (s *Server) rewriteCommentHTML(content string) string {
	content = commentGalleryHrefRE.ReplaceAllStringFunc(content, func(m string) string {
		parts := commentGalleryHrefRE.FindStringSubmatch(m)
		if len(parts) != 6 {
			return m
		}
		return parts[1] + html.EscapeString(s.builder().Href("/opds/v2.0/gallery/"+parts[3]+"/"+parts[4])) + parts[5]
	})
	return commentImageTokenRE.ReplaceAllStringFunc(content, func(m string) string {
		parts := commentImageTokenRE.FindStringSubmatch(m)
		if len(parts) != 3 {
			return m
		}
		u, err := url.Parse(parts[2])
		if err != nil || !contains(s.Settings.ImageProxyHosts, strings.ToLower(u.Host)) || u.Port() != "" || u.User != nil {
			return m
		}
		return parts[1] + html.EscapeString(s.builder().Href("/image/fetch?url="+quote(parts[2])))
	})
}
func isoNow() string { return feed.FormatDate("") }

var _ = sort.Strings
