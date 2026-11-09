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
)

var cursorRE = regexp.MustCompile(`^\d+(?:-\d+)*$`)
var periods = map[string]string{"yesterday": "Yesterday", "month": "Past Month", "year": "Past Year", "alltime": "All Time"}

const homepagePreviewDeadline = 12 * time.Second

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
		info, e = s.Service.Favorites(ctx, next, "")
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
	}
	writeBytes(w, 200, feed.MIMEAtomAcq, map[string]string{"Cache-Control": "public, max-age=300"}, []byte(s.builder().AtomList(info.Galleries, title, query, query, nextHref, nil)))
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
	facets := [][3]string{}
	for p, label := range periods {
		facets = append(facets, [3]string{label, s.builder().Href("/opds/v1.2/toplist?period=" + p), fmt.Sprint(p == period)})
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
	writeBytes(w, 200, feed.MIMEAtomAcq, map[string]string{"Cache-Control": "public, max-age=300"}, []byte(s.builder().AtomChapter(gid, token, title, d.PublishTime, author, d.Category, d.ImageCount)))
}

func (s *Server) searchXML20(w http.ResponseWriter, r *http.Request) {
	writeBytes(w, 200, feed.MIMEOpenSearch, map[string]string{"Cache-Control": "public, max-age=3600"}, []byte(s.builder().OpenSearch("/opds/v2.0/gallery")))
}
func (s *Server) gallery20(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query, next, category := q.Get("query"), q.Get("next"), q.Get("category")
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
		cat := ""
		if strings.HasPrefix(query, "favorites:") {
			cat = strings.TrimPrefix(query, "favorites:")
			if !regexp.MustCompile(`^\d+$`).MatchString(cat) {
				writeJSON(w, 400, map[string]any{"detail": fmt.Sprintf("unknown favorites folder %q (expected favorites or favorites:N)", cat)})
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
		if query != "" {
			params = append(params, "query="+quote(query))
		}
		if category != "" {
			params = append(params, "category="+quote(category))
		}
		if len(params) > 0 {
			nextHref += "&" + strings.Join(params, "&")
		}
	}
	var facets []map[string]any
	if query != "popular" && query != "watched" && query != "favorites" && !strings.HasPrefix(query, "favorites:") {
		facets = s.builder().CategoryFacets()
	}
	doc := s.builder().Acquisition(title, "urn:ehentai:gallery-list:"+firstNonEmpty(query, "latest"), "/opds/v2.0/gallery", pubs, nextHref, facets)
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
	groupTitles := map[string]string{}
	for _, g := range cfg.Groups {
		groupTitles[g.ID] = g.Title
	}
	grouped := map[string][]home.Section{}
	groupOrder := []string{}
	rootNav := []map[string]any{}
	outGroups := []map[string]any{}
	emitted := map[string]bool{}
	for _, sec := range sections {
		if sec.Group != "" {
			if _, ok := grouped[sec.Group]; !ok {
				groupOrder = append(groupOrder, sec.Group)
			}
			grouped[sec.Group] = append(grouped[sec.Group], sec)
			continue
		}
		if sec.Kind == "publication" {
			grouped["__single_"+sec.Query] = []home.Section{sec}
			groupOrder = append(groupOrder, "__single_"+sec.Query)
		} else {
			title := sec.Title
			rootNav = append(rootNav, map[string]any{"title": title, "href": s.builder().Href(home.BuildHref(sec.Type, sec.Query)), "rel": "subsection", "type": feed.MIMEOPDSAcq})
		}
	}
	previews, previewTimedOut := s.fetchHomePreviews(r.Context(), sections)
	for _, gid := range groupOrder {
		if emitted[gid] {
			continue
		}
		emitted[gid] = true
		list := grouped[gid]
		if len(list) == 0 {
			continue
		}
		first := list[0]
		title := groupTitles[gid]
		if strings.HasPrefix(gid, "__single_") {
			title = first.Title
		}
		if title == "" {
			title = first.Title
		}
		g := map[string]any{"metadata": map[string]any{"title": title, "identifier": "urn:ehentai:group:" + gid, "modified": isoNow()}, "links": []map[string]any{{"rel": "self", "href": s.builder().Href(home.BuildHref(first.Type, first.Query)), "type": feed.MIMEOPDSAcq, "title": title}}}
		pubs := []map[string]any{}
		nav := []map[string]any{}
		for _, sec := range list {
			if sec.Kind == "publication" {
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
						s.Logger.Printf("home section %q: %v", sec.Title, err)
					}
					continue
				}
				info := result.info
				if count > len(info.Galleries) {
					count = len(info.Galleries)
				}
				for _, item := range info.Galleries[:count] {
					pubs = append(pubs, s.builder().Publication(item, false))
				}
			} else {
				nav = append(nav, map[string]any{"title": firstNonEmpty(sec.Title, sec.Query), "href": s.builder().Href(home.BuildHref(sec.Type, sec.Query)), "rel": "subsection", "type": feed.MIMEOPDSAcq})
			}
		}
		if len(pubs) > 0 {
			g["publications"] = pubs
		}
		if len(nav) > 0 {
			g["navigation"] = nav
		}
		outGroups = append(outGroups, g)
	}
	doc := s.builder().NavigationDocument(rootNav, outGroups)
	cacheControl := "public, max-age=300"
	if previewTimedOut {
		cacheControl = "no-store"
	}
	writeBytes(w, 200, feed.MIMEOPDSNav, map[string]string{"Cache-Control": cacheControl}, s.builder().JSON(doc))
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
func (s *Server) detailPublication(r *http.Request, gid int, token string) (map[string]any, error) {
	d, e := s.Service.GetDetail(r.Context(), gid, token)
	if e != nil {
		return nil, e
	}
	title, authors := feed.ParseDetailTitle(d.Title, d.TitleJPN)
	count := d.ImageCount
	if s.Store.IsReady(gid, token) {
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
	item := eh.GalleryListItem{GID: gid, Token: token, Title: title, PageCount: &count, Language: d.Language, PublishTime: d.PublishTime, Tags: d.Tags, Category: d.Category}
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
	pub["links"] = s.builder().DetailLinks(gid, token, title, count)
	if count > 0 {
		pub["readingOrder"] = s.builder().ReadingOrder(gid, token, count)
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
