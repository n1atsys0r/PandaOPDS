package app

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"math"
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

func isWatchedQuery(query string) bool {
	return query == "watched" || strings.HasPrefix(query, "watched ")
}

func isFavoritesQuery(query string) bool {
	return query == "favorites" || strings.HasPrefix(query, "favorites:")
}

var galleryFilterParamKeys = []string{"f_cats", "advsearch", "f_sr", "f_srdd", "f_sh", "f_sfl", "f_sfu", "f_sft", "f_sp", "f_spf", "f_spt"}

func copyGalleryFilterParams(dst, src url.Values) {
	for _, key := range galleryFilterParamKeys {
		if values := src[key]; len(values) > 0 {
			dst[key] = append([]string(nil), values...)
		}
	}
}

func galleryFilterParams(src url.Values) url.Values {
	dst := url.Values{}
	copyGalleryFilterParams(dst, src)
	return dst
}

func validFavcat(favcat string) bool {
	if favcat == "all" {
		return true
	}
	id, err := strconv.Atoi(favcat)
	return err == nil && id >= 0 && id <= 9
}

func allFavoritesCategory(favcat string) string {
	if favcat == "" {
		return "all"
	}
	return favcat
}

func searchDescriptionHref12(query, favcat string) string {
	params := url.Values{}
	switch {
	case query == "watched":
		params.Set("query", "watched")
	case query == "favorites":
		params.Set("query", "favorites")
		params.Set("favcat", allFavoritesCategory(favcat))
	default:
		return "/opds/v1.2/search.xml"
	}
	return "/opds/v1.2/search.xml?" + params.Encode()
}

func gallerySelfHref12(builder feed.Builder, query, next, favcat, searchTerm string, filters url.Values) string {
	params := url.Values{}
	if query != "" {
		params.Set("query", query)
	}
	if next != "" {
		params.Set("next", next)
	}
	if favcat != "" {
		params.Set("favcat", favcat)
	}
	if searchTerm != "" {
		params.Set("f_search", searchTerm)
	}
	copyGalleryFilterParams(params, filters)
	path := "/opds/v1.2/gallery"
	if encoded := params.Encode(); encoded != "" {
		path += "?" + encoded
	}
	return builder.Href(path)
}

func searchTemplateHref20(query, favcat string) string {
	switch {
	case isWatchedQuery(query):
		return "/opds/v2.0/gallery?query=watched&f_search={searchTerms}"
	case isFavoritesQuery(query):
		if favcat == "" && strings.HasPrefix(query, "favorites:") {
			favcat = strings.TrimPrefix(query, "favorites:")
		}
		return "/opds/v2.0/gallery?query=favorites&favcat=" + quote(allFavoritesCategory(favcat)) + "&f_search={searchTerms}"
	default:
		return "/opds/v2.0/gallery?query={searchTerms}"
	}
}

func favoriteFacetPath(route, favcat string, searchTerm ...string) string {
	term := ""
	if len(searchTerm) > 0 {
		term = searchTerm[0]
	}
	return favoriteFacetPathWithFilters(route, favcat, term, nil)
}

func favoriteFacetPathWithFilters(route, favcat, searchTerm string, filters url.Values) string {
	values := url.Values{"query": {"favorites"}}
	if favcat != "" {
		values.Set("favcat", favcat)
	}
	if searchTerm != "" {
		values.Set("f_search", searchTerm)
	}
	copyGalleryFilterParams(values, filters)
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

func favoriteAtomFacets(builder feed.Builder, route, selected string, names map[int]string, searchTerm string, filters url.Values) []feed.AtomFacet {
	facets := []feed.AtomFacet{}
	for id := 0; id < 10; id++ {
		value := strconv.Itoa(id)
		facets = append(facets, feed.AtomFacet{Title: favoriteFacetTitle(names, id), Href: builder.Href(favoriteFacetPathWithFilters(route, value, searchTerm, filters)), Group: "Favorite category", Active: selected == value})
	}
	return facets
}

func favoriteJSONFacets(builder feed.Builder, route, selected string, names map[int]string, searchTerm string, filters url.Values) []map[string]any {
	return []map[string]any{{"metadata": map[string]string{"title": "Favorite category"}, "links": favoriteCategoryLinksWithFilters(builder, route, selected, names, searchTerm, filters)}}
}

func favoriteCategoryLinks(builder feed.Builder, route, selected string, names map[int]string, searchTerm ...string) []map[string]any {
	term := ""
	if len(searchTerm) > 0 {
		term = searchTerm[0]
	}
	return favoriteCategoryLinksWithFilters(builder, route, selected, names, term, nil)
}

func favoriteCategoryLinksWithFilters(builder feed.Builder, route, selected string, names map[int]string, searchTerm string, filters url.Values) []map[string]any {
	links := []map[string]any{}
	for id := 0; id < 10; id++ {
		favcat := strconv.Itoa(id)
		title := favoriteFacetTitle(names, id)
		link := map[string]any{"href": builder.Href(favoriteFacetPathWithFilters(route, favcat, searchTerm, filters)), "title": title}
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
	q := r.URL.Query()
	template := "/opds/v1.2/gallery?query={searchTerms}"
	switch q.Get("query") {
	case "watched":
		template = "/opds/v1.2/gallery?query=watched&f_search={searchTerms}"
	case "favorites":
		favcat := allFavoritesCategory(q.Get("favcat"))
		if !validFavcat(favcat) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "unknown favorites folder (expected all or favcat 0-9)"})
			return
		}
		template = "/opds/v1.2/gallery?query=favorites&favcat=" + quote(favcat) + "&f_search={searchTerms}"
	}
	writeBytes(w, 200, feed.MIMEOpenSearch, map[string]string{"Cache-Control": "public, max-age=3600"}, []byte(s.builder().OpenSearchTemplate(template)))
}
func (s *Server) gallery12(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := q.Get("query")
	next := q.Get("next")
	favcat := q.Get("favcat")
	fSearch := q.Get("f_search")
	if query == "favorites" && favcat == "" && fSearch != "" {
		favcat = "all"
	}
	if query == "favorites" && favcat != "" && !validFavcat(favcat) {
		writeJSON(w, 400, map[string]any{"detail": "unknown favorites folder (expected all or favcat 0-9)"})
		return
	}
	if next != "" && !cursorRE.MatchString(next) {
		writeJSON(w, 400, map[string]any{"detail": "invalid next cursor (expected digits or gid-favtime)"})
		return
	}
	var info eh.GalleryPageInfo
	var e error
	ctx := r.Context()
	extra := galleryFilterParams(q)
	switch query {
	case "popular":
		info, e = s.Service.Popular(ctx, next)
	case "watched":
		if fSearch != "" {
			extra.Set("f_search", fSearch)
		}
		info, e = s.Service.WatchedQuery(ctx, query, next, extra)
	case "favorites":
		info, e = s.Service.FavoritesSearch(ctx, next, favcat, fSearch, extra)
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
	if query == "watched" && fSearch != "" {
		title += " — " + fSearch
	} else if query == "favorites" && fSearch != "" {
		title += " — " + fSearch
	}
	title = "E-Hentai: " + title
	nextHref := ""
	if info.NextGID != "" {
		params := url.Values{"next": {info.NextGID}}
		if query != "" {
			params.Set("query", query)
		}
		if query == "favorites" {
			if favcat != "" {
				params.Set("favcat", favcat)
			}
			if fSearch != "" {
				params.Set("f_search", fSearch)
			}
		}
		if query == "watched" && fSearch != "" {
			params.Set("f_search", fSearch)
		}
		copyGalleryFilterParams(params, q)
		nextHref = s.builder().Href("/opds/v1.2/gallery?" + params.Encode())
	}
	var facets []feed.AtomFacet
	if query == "favorites" {
		names := s.favoriteFacetNames(ctx, info.FavcatMap)
		facets = favoriteAtomFacets(s.builder(), "/opds/v1.2/gallery", favcat, names, fSearch, extra)
	}
	searchHref := searchDescriptionHref12(query, favcat)
	selfHref := ""
	if query == "watched" || query == "favorites" {
		selfHref = gallerySelfHref12(s.builder(), query, next, favcat, fSearch, extra)
	}
	writeBytes(w, 200, feed.MIMEAtomAcq, map[string]string{"Cache-Control": "public, max-age=300"}, []byte(s.builder().AtomListWithLinks(info.Galleries, title, query, query, nextHref, facets, searchHref, selfHref)))
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

func (s *Server) history12(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	info, err := s.HistoryGalleries(r.Context(), page, 20)
	if err != nil {
		writeError(w, err)
		return
	}
	next := ""
	if info.NextPage != nil {
		next = s.builder().Href(fmt.Sprintf("/opds/v1.2/history?page=%d", *info.NextPage))
	}
	writeBytes(w, 200, feed.MIMEAtomAcq, map[string]string{"Cache-Control": "public, max-age=60"}, []byte(s.builder().AtomList(info.Galleries, "E-Hentai: Recently Read", "history", "", next, nil)))
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
		cnt := d.ImageCount
		snap := progression.Snapshot{
			Title:       d.Title,
			TitleJPN:    d.TitleJPN,
			Category:    d.Category,
			CoverURL:    d.CoverURL,
			PageCount:   &cnt,
			Rating:      d.Rating,
			PublishTime: d.PublishTime,
		}
		_ = s.Progression.SetSnapshot(gid, token, snap)
		if activeGID != gid || activeToken != token {
			_ = s.Progression.SetSnapshot(activeGID, activeToken, snap)
		}
	}
	writeBytes(w, 200, feed.MIMEAtomAcq, map[string]string{"Cache-Control": "public, max-age=300"}, []byte(s.builder().AtomChapterVersion(gid, token, item, title, author)))
}

func (s *Server) searchXML20(w http.ResponseWriter, r *http.Request) {
	writeBytes(w, 200, feed.MIMEOpenSearch, map[string]string{"Cache-Control": "public, max-age=3600"}, []byte(s.builder().OpenSearch("/opds/v2.0/gallery")))
}
func (s *Server) gallery20(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query, next, category, favcat := q.Get("query"), q.Get("next"), q.Get("category"), q.Get("favcat")
	fSearch := q.Get("f_search")
	if query == "favorites" && favcat == "" && fSearch != "" {
		favcat = "all"
	}
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
	for _, k := range galleryFilterParamKeys {
		if v := q.Get(k); v != "" {
			extra.Set(k, v)
		}
	}
	var info eh.GalleryPageInfo
	var e error
	ctx := r.Context()
	switch {
	case query == "popular":
		info, e = s.Service.Popular(ctx, next)
	case query == "watched" || strings.HasPrefix(query, "watched "):
		if query == "watched" && fSearch != "" {
			extra.Set("f_search", fSearch)
		}
		info, e = s.Service.WatchedQuery(ctx, query, next, extra)
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
		if cat != "" && !validFavcat(cat) {
			writeJSON(w, 400, map[string]any{"detail": "unknown favorites folder (expected all or favcat 0-9)"})
			return
		}
		if cat == "" && fSearch != "" {
			cat = "all"
		}
		info, e = s.Service.FavoritesSearch(ctx, next, cat, fSearch, extra)
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
	switch {
	case query == "":
		name = "Latest"
	case query == "popular", query == "favorites":
		name = strings.ToUpper(query[:1]) + query[1:]
		if query == "favorites" && fSearch != "" {
			name += " — " + fSearch
		}
	case query == "watched" || strings.HasPrefix(query, "watched "):
		name = "Watched"
		if term := strings.TrimSpace(strings.TrimPrefix(query, "watched")); term != "" {
			name += " — " + term
		} else if fSearch != "" {
			name += " — " + fSearch
		}
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
			if fSearch != "" {
				params = append(params, "f_search="+quote(fSearch))
			}
		} else if query != "" {
			params = append(params, "query="+quote(query))
			if query == "watched" && fSearch != "" {
				params = append(params, "f_search="+quote(fSearch))
			}
		}
		if favcat != "" && query != "favorites" && !strings.HasPrefix(query, "favorites:") {
			params = append(params, "favcat="+quote(favcat))
		}
		if category != "" {
			params = append(params, "category="+quote(category))
		}
		for _, k := range galleryFilterParamKeys {
			if v := q.Get(k); v != "" {
				params = append(params, k+"="+quote(v))
			}
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
		facets = favoriteJSONFacets(s.builder(), "/opds/v2.0/gallery", selected, names, fSearch, extra)
	} else if query != "popular" {
		cfg, _ := home.Load(s.Settings.HomeConfigPath)
		searchFacets := cfg.GetSearchFacets()
		if len(searchFacets) > 0 {
			facets = []map[string]any{
				{
					"metadata": map[string]string{"title": "Filter"},
					"links":    s.customFacetLinks(searchFacets, "search", query, r.URL),
				},
			}
		}
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
		if fSearch != "" {
			self += "&f_search=" + quote(fSearch)
		}
	} else if query != "" {
		self += "?query=" + quote(query)
		if query == "watched" && fSearch != "" {
			self += "&f_search=" + quote(fSearch)
		}
		if favcat != "" {
			self += "&favcat=" + quote(favcat)
		}
		if category != "" {
			self += "&category=" + quote(category)
		}
	} else if category != "" {
		self += "?category=" + quote(category)
	}
	for _, k := range galleryFilterParamKeys {
		if v := q.Get(k); v != "" {
			if strings.Contains(self, "?") {
				self += "&" + k + "=" + quote(v)
			} else {
				self += "?" + k + "=" + quote(v)
			}
		}
	}
	doc := s.builder().AcquisitionWithSearch(title, "urn:ehentai:gallery-list:"+firstNonEmpty(query, "latest"), self, pubs, nextHref, facets, searchTemplateHref20(query, favcat))
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
	title := "E-Hentai: Toplist " + periods[period]
	doc := s.builder().Acquisition(title, "urn:ehentai:toplist:"+period, "/opds/v2.0/toplist?period="+period, pubs, next, nil)
	writeBytes(w, 200, feed.MIMEOPDSAcq, map[string]string{"Cache-Control": "public, max-age=300"}, s.builder().JSON(doc))
}
func (s *Server) archives20(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	category := r.URL.Query().Get("category")

	info, err := s.ArchivedGalleriesFiltered(page, 100, category, "")
	if err != nil {
		writeError(w, err)
		return
	}
	pubs := []map[string]any{}
	for _, item := range info.Galleries {
		pubs = append(pubs, s.builder().Publication(item, false))
	}
	next := ""
	if info.NextPage != nil {
		nextURL := fmt.Sprintf("/opds/v2.0/archives?page=%d", *info.NextPage)
		if category != "" {
			nextURL += "&category=" + url.QueryEscape(category)
		}
		next = s.builder().Href(nextURL)
	}
	doc := s.builder().Acquisition("E-Hentai: Archives", "urn:ehentai:archives", "/opds/v2.0/archives", pubs, next, nil)
	writeBytes(w, 200, feed.MIMEOPDSAcq, map[string]string{"Cache-Control": "public, max-age=300"}, s.builder().JSON(doc))
}

func (s *Server) history20(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	category := r.URL.Query().Get("category")

	info, err := s.HistoryGalleriesFiltered(r.Context(), page, 20, category, "")
	if err != nil {
		writeError(w, err)
		return
	}
	pubs := []map[string]any{}
	for _, item := range info.Galleries {
		pubs = append(pubs, s.builder().Publication(item, false))
	}
	next := ""
	if info.NextPage != nil {
		nextURL := fmt.Sprintf("/opds/v2.0/history?page=%d", *info.NextPage)
		if category != "" {
			nextURL += "&category=" + url.QueryEscape(category)
		}
		next = s.builder().Href(nextURL)
	}
	doc := s.builder().Acquisition("E-Hentai: Recently Read", "urn:ehentai:history", "/opds/v2.0/history", pubs, next, nil)
	writeBytes(w, 200, feed.MIMEOPDSAcq, map[string]string{"Cache-Control": "public, max-age=60"}, s.builder().JSON(doc))
}
func (s *Server) pageV20(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" || id == "home" {
		s.rootV20(w, r)
		return
	}
	cfg, err := home.Load(s.Settings.HomeConfigPath)
	if err != nil && s.Logger != nil {
		s.Logger.Printf("home config load failed, using defaults: %v", err)
	}
	page, ok := cfg.FindPage(id)
	if !ok {
		writeJSON(w, 404, map[string]any{"detail": "page not found"})
		return
	}
	s.renderPageV20(w, r, cfg, page, false)
}

func (s *Server) rootV20(w http.ResponseWriter, r *http.Request) {
	cfg, e := home.Load(s.Settings.HomeConfigPath)
	if e != nil && s.Logger != nil {
		s.Logger.Printf("home config load failed, using defaults: %v", e)
	}
	s.renderPageV20(w, r, cfg, cfg.HomePage(), true)
}

func (s *Server) renderPageV20(w http.ResponseWriter, r *http.Request, cfg home.Config, p home.Page, isRoot bool) {
	sections := []home.Section{}
	for _, sec := range p.Sections {
		if sec.Kind == "navigation" && sec.Group && len(sec.Items) > 0 {
			sections = append(sections, sec)
			continue
		}
		if home.AuthRequired(sec) && !s.Settings.HasIPB() {
			continue
		}
		if sec.Type == "preset" && sec.Query == "archives" && s.Store.ReadyCount() == 0 {
			continue
		}
		sections = append(sections, sec)
	}
	rootNav := []map[string]any{}
	for _, sec := range cfg.GlobalNavigation() {
		if home.AuthRequired(sec) && !s.Settings.HasIPB() {
			continue
		}
		if sec.Type == "preset" && sec.Query == "archives" && s.Store.ReadyCount() == 0 {
			continue
		}
		title := sec.Title
		if title == "" {
			if sec.Type == "page" {
				if p, ok := cfg.FindPage(sec.Query); ok && p.Title != "" {
					title = p.Title
				}
			}
			if title == "" {
				title = sec.Query
			}
		}
		href := s.builder().Href(home.BuildHref(sec.Type, sec.Query))
		rootNav = append(rootNav, map[string]any{
			"title": title,
			"href":  href,
			"rel":   "subsection",
			"type":  feed.MIMEOPDSAcq,
		})
	}
	outGroups := []map[string]any{}
	previews, previewTimedOut := s.fetchHomePreviews(r.Context(), sections)
	rootPubs := []map[string]any{}
	var rootSource homePreviewKey
	for _, sec := range sections {
		title := sec.Title
		if title == "" {
			if sec.Type == "page" {
				if p, ok := cfg.FindPage(sec.Query); ok && p.Title != "" {
					title = p.Title
				}
			}
			if title == "" {
				title = sec.Query
			}
		}
		href := s.builder().Href(home.BuildHref(sec.Type, sec.Query))
		if sec.Kind == "navigation" {
			if sec.Group {
				navLinks := []map[string]any{}
				items := sec.Items
				if len(items) == 0 && (sec.Type != "" || sec.Query != "") {
					items = []home.NavigationItem{{Title: sec.Title, Type: sec.Type, Query: sec.Query}}
				}
				for _, it := range items {
					if home.AuthRequired(home.Section{Type: it.Type, Query: it.Query}) && !s.Settings.HasIPB() {
						continue
					}
					if it.Type == "preset" && it.Query == "archives" && s.Store.ReadyCount() == 0 {
						continue
					}
					itTitle := it.Title
					if itTitle == "" {
						if it.Type == "page" {
							if p, ok := cfg.FindPage(it.Query); ok && p.Title != "" {
								itTitle = p.Title
							}
						}
						if itTitle == "" {
							itTitle = it.Query
						}
					}
					itHref := s.builder().Href(home.BuildHref(it.Type, it.Query))
					navLinks = append(navLinks, map[string]any{
						"title": itTitle,
						"href":  itHref,
						"rel":   "subsection",
						"type":  feed.MIMEOPDSAcq,
					})
				}
				if len(navLinks) > 0 {
					outGroups = append(outGroups, map[string]any{
						"metadata": map[string]any{
							"title":    firstNonEmpty(sec.Title, "Navigation"),
							"modified": isoNow(),
						},
						"navigation": navLinks,
					})
				}
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
				s.Logger.Printf("page section %q: %v", title, err)
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
		if sec.Kind != "facets" {
			continue
		}
		if sec.FacetSet != "" {
			fs := cfg.GetFacetSet(sec.FacetSet)
			if fs != nil && len(fs.Items) > 0 {
				links := s.customFacetLinks(fs.Items, rootSource.typeName, rootSource.query, r.URL)
				if len(links) > 0 {
					rootFacetGroups = append(rootFacetGroups, map[string]any{
						"metadata": map[string]string{"title": firstNonEmpty(sec.Title, firstNonEmpty(fs.Title, "Filter"))},
						"links":    links,
					})
				}
			}
			continue
		}
		if len(sec.Items) > 0 {
			links := s.customFacetLinks(sec.Items, rootSource.typeName, rootSource.query, r.URL)
			if len(links) > 0 {
				rootFacetGroups = append(rootFacetGroups, map[string]any{
					"metadata": map[string]string{"title": firstNonEmpty(sec.Title, "Filter")},
					"links":    links,
				})
			}
			continue
		}
		if len(rootPubs) == 0 || rootSource != (homePreviewKey{sec.Type, sec.Query}) {
			continue
		}
		searchFacets := cfg.GetSearchFacets()
		if len(searchFacets) > 0 {
			links := s.customFacetLinks(searchFacets, rootSource.typeName, rootSource.query, r.URL)
			if len(links) > 0 {
				rootFacetGroups = append(rootFacetGroups, map[string]any{
					"metadata": map[string]string{"title": firstNonEmpty(sec.Title, "Filter")},
					"links":    links,
				})
			}
		}
	}
	if len(rootFacetGroups) == 0 && len(rootPubs) > 0 {
		kind, _, _ := home.ResolvePreset(rootSource.query)
		if rootSource.typeName == "preset" && kind == "favorites" {
			virtualSec := home.Section{Type: rootSource.typeName, Query: rootSource.query}
			links := s.homeFacetLinks(virtualSec, previews[rootSource].info)
			if len(links) >= 1 {
				rootFacetGroups = append(rootFacetGroups, map[string]any{
					"metadata": map[string]string{"title": "Folders"},
					"links":    links,
				})
			}
		} else if rootSource.typeName != "preset" || kind != "toplist" {
			searchFacets := cfg.GetSearchFacets()
			if len(searchFacets) > 0 {
				links := s.customFacetLinks(searchFacets, rootSource.typeName, rootSource.query, r.URL)
				if len(links) > 0 {
					rootFacetGroups = append(rootFacetGroups, map[string]any{
						"metadata": map[string]string{"title": "Filter"},
						"links":    links,
					})
				}
			}
		}
	}

	if isRoot && len(rootNav) == 0 && len(outGroups) == 0 && len(rootPubs) == 0 {
		rootNav = append(rootNav, map[string]any{"title": "Latest", "href": s.builder().Href(home.BuildHref("preset", "latest")), "rel": "subsection", "type": feed.MIMEOPDSAcq})
	}

	doc := s.builder().NavigationDocument(rootNav, outGroups)
	if !isRoot {
		selfHref := s.builder().Href(home.BuildHref("page", p.ID))
		pageTitle := firstNonEmpty(p.Title, p.ID)
		if meta, ok := doc["metadata"].(map[string]any); ok {
			meta["title"] = pageTitle
		}
		newLinks := []map[string]any{
			{"rel": "self", "href": selfHref, "type": feed.MIMEOPDSNav},
			{"rel": "start", "href": s.builder().Href("/opds/v2.0"), "type": feed.MIMEOPDSNav, "title": "Home"},
		}
		if origLinks, ok := doc["links"].([]map[string]any); ok {
			for _, l := range origLinks {
				if l["rel"] != "self" && l["rel"] != "start" {
					newLinks = append(newLinks, l)
				}
			}
		}
		doc["links"] = newLinks
	}
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
func (s *Server) customFacetLinks(items []home.NavigationItem, baseType, baseQuery string, reqURL *url.URL) []map[string]any {
	var links []map[string]any
	targetRoute := "/opds/v2.0/gallery"
	if baseType == "preset" && (baseQuery == "latest" || baseQuery == "") {
		baseQuery = ""
	}

	watchedScope := isWatchedQuery(baseQuery)
	legacyWatchedTerms := ""
	if watchedScope {
		legacyWatchedTerms = strings.TrimSpace(strings.TrimPrefix(baseQuery, "watched"))
		searchTerms := legacyWatchedTerms
		if reqURL != nil && reqURL.Query().Get("f_search") != "" {
			searchTerms = reqURL.Query().Get("f_search")
		}
		baseQuery = stripCustomFacetSuffix(searchTerms, items)
	} else {
		baseQuery = stripCustomFacetSuffix(baseQuery, items)
	}
	requestParams := url.Values{}
	if reqURL != nil {
		for key, values := range reqURL.Query() {
			requestParams[key] = append([]string(nil), values...)
		}
	}
	if watchedScope {
		requestParams.Set("query", "watched")
		if requestParams.Get("f_search") == "" {
			if legacyWatchedTerms != "" {
				requestParams.Set("f_search", legacyWatchedTerms)
			}
		}
	}

	var activeIndex int = -1
	for idx, it := range items {
		params := url.Values{}
		if watchedScope {
			params.Set("query", "watched")
			searchTerms := baseQuery
			if it.Query != "" {
				searchTerms = strings.TrimSpace(strings.Join([]string{searchTerms, it.Query}, " "))
			}
			if searchTerms != "" {
				params.Set("f_search", searchTerms)
			}
		} else {
			combinedQuery := baseQuery
			if it.Query != "" {
				if combinedQuery != "" {
					combinedQuery += " " + it.Query
				} else {
					combinedQuery = it.Query
				}
			}
			if combinedQuery != "" {
				params.Set("query", combinedQuery)
			}
		}
		if len(it.Categories) > 0 {
			mask := eh.CategoryMask(it.Categories, s.Settings)
			if mask > 0 {
				params.Set("f_cats", strconv.Itoa(mask))
			}
		}
		if it.Rating > 0 {
			params.Set("advsearch", "1")
			params.Set("f_sr", "on")
			params.Set("f_srdd", strconv.Itoa(it.Rating))
		}
		if it.Expunged {
			params.Set("advsearch", "1")
			params.Set("f_sh", "on")
		}
		for _, df := range it.DisableFilters {
			switch strings.ToLower(df) {
			case "language":
				params.Set("advsearch", "1")
				params.Set("f_sfl", "on")
			case "uploader":
				params.Set("advsearch", "1")
				params.Set("f_sfu", "on")
			case "tags":
				params.Set("advsearch", "1")
				params.Set("f_sft", "on")
			}
		}

		u := targetRoute
		if len(params) > 0 {
			u += "?" + params.Encode()
		}
		linkHref := s.builder().Href(u)
		link := map[string]any{
			"title": firstNonEmpty(it.Title, "All"),
			"href":  linkHref,
			"type":  feed.MIMEOPDSAcq,
		}
		if reqURL != nil && isParamsMatching(requestParams, params) {
			activeIndex = idx
		}
		links = append(links, link)
	}

	if activeIndex >= 0 && activeIndex < len(links) {
		links[activeIndex]["rel"] = "self"
	}

	return links
}

func stripCustomFacetSuffix(searchTerms string, items []home.NavigationItem) string {
	searchTerms = strings.TrimSpace(searchTerms)
	for {
		matched := ""
		for _, item := range items {
			term := strings.TrimSpace(item.Query)
			if term == "" || len(term) <= len(matched) {
				continue
			}
			if searchTerms == term || strings.HasSuffix(searchTerms, " "+term) {
				matched = term
			}
		}
		if matched == "" {
			return searchTerms
		}
		searchTerms = strings.TrimSpace(strings.TrimSuffix(searchTerms, matched))
	}
}

func isParamsMatching(reqQuery, targetParams url.Values) bool {
	keys := append([]string{"query", "f_search"}, galleryFilterParamKeys...)
	for _, k := range keys {
		if reqQuery.Get(k) != targetParams.Get(k) {
			return false
		}
	}
	return true
}

func (s *Server) homeFacetLinks(sec home.Section, info eh.GalleryPageInfo) []map[string]any {
	builder := s.builder()
	const galleryRoute = "/opds/v2.0/gallery"
	if sec.Query == "archives" || sec.Query == "history" {
		return nil
	}
	if sec.Type == "search" || sec.Query == "latest" || sec.Query == "watched" {
		targetRoute := galleryRoute
		query := ""
		if sec.Type == "search" {
			query = sec.Query
		} else if sec.Query == "watched" {
			query = "watched"
		}
		groups := builder.CategoryFacets(targetRoute, query, "")
		if len(groups) > 0 {
			if links, ok := groups[0]["links"].([]map[string]any); ok {
				return links
			}
		}
		return nil
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
		return s.Service.Watched(ctx, "", nil)
	case "favorites":
		return s.Service.Favorites(ctx, "", arg)
	case "archives":
		return eh.GalleryPageInfo{Galleries: s.Service.ArchivedGalleries(1)}, nil
	case "history":
		return s.HistoryGalleries(ctx, 1, 20)
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
	pub, _, e := s.detailPublication(r, gid, token)
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
	pub, _, e := s.detailPublication(r, gid, token)
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
	pub, detail, e := s.detailPublication(r, gid, token)
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
	if detail.Favcat != nil && *detail.Favcat >= 0 && *detail.Favcat <= 9 {
		name := strings.TrimSpace(detail.FavcatName)
		if name == "" {
			name = favoriteFacetTitle(nil, *detail.Favcat)
		}
		metadata["belongsTo"] = map[string]any{
			"collection": map[string]any{
				"name": name,
				"links": []map[string]string{{
					"href": s.builder().Href(favoriteFacetPath("/opds/v2.0/gallery", strconv.Itoa(*detail.Favcat))),
					"type": feed.MIMEPublication,
				}},
			},
		}
	}
	writeBytes(w, http.StatusOK, feed.MIMEDivina, map[string]string{"Cache-Control": "public, max-age=300"}, s.builder().JSON(pub))
}

func (s *Server) detailPublication(r *http.Request, gid int, token string) (map[string]any, eh.DetailPageInfo, error) {
	d, e := s.Service.GetDetail(r.Context(), gid, token)
	if e != nil {
		return nil, eh.DetailPageInfo{}, e
	}
	title, authors := feed.ParseDetailTitle(d.Title, d.TitleJPN)
	count := d.ImageCount
	activeGID, activeToken := d.ActiveGID, d.ActiveToken
	if activeGID <= 0 || activeToken == "" {
		activeGID, activeToken = gid, token
	}
	if s.Progression != nil {
		observeProgressionVersions(s.Progression, gid, token, activeGID, activeToken, d.NewerVersions)
		cnt := d.ImageCount
		snap := progression.Snapshot{
			Title:       d.Title,
			TitleJPN:    d.TitleJPN,
			Category:    d.Category,
			CoverURL:    d.CoverURL,
			PageCount:   &cnt,
			Rating:      d.Rating,
			PublishTime: d.PublishTime,
		}
		_ = s.Progression.SetSnapshot(gid, token, snap)
		if activeGID != gid || activeToken != token {
			_ = s.Progression.SetSnapshot(activeGID, activeToken, snap)
		}
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
	return pub, d, nil
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

func (s *Server) HistoryGalleries(ctx context.Context, page, pageSize int) (eh.GalleryPageInfo, error) {
	return s.HistoryGalleriesFiltered(ctx, page, pageSize, "", "")
}

func (s *Server) HistoryGalleriesFiltered(ctx context.Context, page, pageSize int, category, filter string) (eh.GalleryPageInfo, error) {
	if s.Progression == nil {
		return eh.GalleryPageInfo{}, nil
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * pageSize
	predicate := func(it progression.HistoryItem) bool {
		if filter != "" && !progression.MatchReadingStatus(true, it.Doc.Progression, filter) {
			return false
		}
		if category != "" {
			cat := ""
			if it.Snapshot != nil {
				cat = it.Snapshot.Category
			} else if s.Store != nil {
				if snap, ok := s.Store.GetMetadataSnapshot(it.GID, it.Token); ok {
					cat = toString(snap["category"])
				}
			}
			if cat == "" {
				cat = "Misc"
			}
			if !strings.EqualFold(cat, category) {
				return false
			}
		}
		return true
	}
	items, total, err := s.Progression.ListHistoryFiltered(offset, pageSize, predicate)
	if err != nil {
		return eh.GalleryPageInfo{}, err
	}
	galleries := make([]eh.GalleryListItem, 0, len(items))
	var missingList []eh.GalleryRef
	for _, it := range items {
		if it.Snapshot != nil {
			title := it.Snapshot.Title
			if it.Snapshot.TitleJPN != "" {
				title = it.Snapshot.TitleJPN
			}
			galleries = append(galleries, eh.GalleryListItem{
				GID:         it.GID,
				Token:       it.Token,
				Title:       title,
				Category:    it.Snapshot.Category,
				CoverURL:    it.Snapshot.CoverURL,
				PageCount:   it.Snapshot.PageCount,
				Rating:      it.Snapshot.Rating,
				PublishTime: it.Snapshot.PublishTime,
				Language:    it.Snapshot.Language,
				Expunged:    it.Snapshot.Expunged,
			})
		} else {
			// Snapshot is missing: check archive first
			if s.Store != nil {
				if snap, ok := s.Store.GetMetadataSnapshot(it.GID, it.Token); ok {
					title := toString(snap["title_jpn"])
					if title == "" {
						title = toString(snap["title"])
					}
					cat := toString(snap["category"])
					rating := toFloat(snap["rating"])
					count := int(toInt64(snap["filecount"]))
					publishTime := ""
					if posted := toInt64(snap["posted"]); posted > 0 {
						publishTime = time.Unix(posted, 0).UTC().Format(time.RFC3339)
					}
					cov := ""
					if thumb := toString(snap["thumb"]); thumb != "" {
						cov = thumb
					}
					snapshot := progression.Snapshot{
						Title:       toString(snap["title"]),
						TitleJPN:    toString(snap["title_jpn"]),
						Category:    cat,
						CoverURL:    cov,
						PageCount:   &count,
						Rating:      rating,
						PublishTime: publishTime,
					}
					_ = s.Progression.SetSnapshot(it.GID, it.Token, snapshot)
					galleries = append(galleries, eh.GalleryListItem{
						GID:         it.GID,
						Token:       it.Token,
						Title:       title,
						Category:    cat,
						CoverURL:    cov,
						PageCount:   &count,
						Rating:      rating,
						PublishTime: publishTime,
					})
					continue
				}
			}
			missingList = append(missingList, eh.GalleryRef{GID: it.GID, Token: it.Token})
			title := fmt.Sprintf("Gallery #%d", it.GID)
			if it.Doc.Title != nil && *it.Doc.Title != "" {
				title = *it.Doc.Title
			}
			galleries = append(galleries, eh.GalleryListItem{
				GID:      it.GID,
				Token:    it.Token,
				Title:    title,
				Category: "Misc",
			})
		}
	}

	if len(missingList) > 0 && s.Service != nil && s.Service.Client != nil {
		go func(missing []eh.GalleryRef) {
			bCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			results, err := s.Service.Client.GDataBatch(bCtx, missing)
			if err == nil {
				for _, g := range results {
					count := g.Filecount
					publishTime := ""
					if g.Posted > 0 {
						publishTime = time.Unix(g.Posted, 0).UTC().Format(time.RFC3339)
					}
					snap := progression.Snapshot{
						Title:       g.Title,
						TitleJPN:    g.TitleJPN,
						Category:    g.Category,
						CoverURL:    g.Thumb,
						PageCount:   &count,
						Rating:      g.Rating,
						PublishTime: publishTime,
						Language:    g.Language(),
						Expunged:    g.Expunged,
					}
					_ = s.Progression.SetSnapshot(g.GID, g.Token, snap)
				}
			}
		}(missingList)
	}

	var nextPage *int
	if offset+len(galleries) < total {
		np := page + 1
		nextPage = &np
	}
	return eh.GalleryPageInfo{
		Galleries:  galleries,
		TotalCount: &total,
		NextPage:   nextPage,
	}, nil
}

func toString(v any) string { s, _ := v.(string); return s }

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}

func (s *Server) ArchivedGalleriesFiltered(page, pageSize int, category, filter string) (eh.GalleryPageInfo, error) {
	if s.Store == nil || s.Service == nil {
		return eh.GalleryPageInfo{}, nil
	}
	if pageSize <= 0 {
		pageSize = 100
	}
	if page < 1 {
		page = 1
	}

	var progressMap map[string]float64
	if filter != "" && s.Progression != nil {
		progressMap, _ = s.Progression.GetProgressions()
	}

	all := s.Store.ListReady(1, math.MaxInt)
	var matched []eh.ArchiveInfo
	for _, a := range all {
		if filter != "" {
			activeGID, activeToken := a.ActiveGID, a.ActiveToken
			if activeGID <= 0 || activeToken == "" {
				if s.Service != nil {
					activeGID, activeToken, _ = s.Service.ResolveArchiveVersion(a.GID, a.Token)
				}
				if activeGID <= 0 || activeToken == "" {
					activeGID, activeToken = a.GID, a.Token
				}
			}
			p, has := progressMap[fmt.Sprintf("%d/%s", activeGID, strings.ToLower(activeToken))]
			if !has {
				p, has = progressMap[fmt.Sprintf("%d/%s", a.GID, strings.ToLower(a.Token))]
			}
			if !progression.MatchReadingStatus(has, p, filter) {
				continue
			}
		}
		if category != "" {
			snap, ok := s.Store.GetMetadataSnapshot(a.ActiveGID, a.ActiveToken)
			if !ok {
				snap, _ = s.Store.GetMetadataSnapshot(a.GID, a.Token)
			}
			cat := ""
			if snap != nil {
				cat = toString(snap["category"])
			}
			if !strings.EqualFold(cat, category) {
				continue
			}
		}
		matched = append(matched, a)
	}

	total := len(matched)
	start := (page - 1) * pageSize
	if start >= total {
		return eh.GalleryPageInfo{TotalCount: &total}, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	galleries := make([]eh.GalleryListItem, 0, end-start)
	for _, a := range matched[start:end] {
		galleries = append(galleries, s.Service.ArchivedGalleryItem(a))
	}

	var nextPage *int
	if end < total {
		np := page + 1
		nextPage = &np
	}
	return eh.GalleryPageInfo{
		Galleries:  galleries,
		TotalCount: &total,
		NextPage:   nextPage,
	}, nil
}
