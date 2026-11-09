package feed

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"pandaopds/internal/config"
	"pandaopds/internal/eh"
)

const (
	MIMEAtomNav     = "application/atom+xml;profile=opds-catalog;kind=navigation"
	MIMEAtomAcq     = "application/atom+xml;profile=opds-catalog;kind=acquisition"
	MIMEOpenSearch  = "application/opensearchdescription+xml"
	MIMEOPDSNav     = "application/opds+json;profile=navigation"
	MIMEOPDSAcq     = "application/opds+json;profile=acquisition"
	MIMEPublication = "application/opds+json"
	MIMEImage       = "image/jpeg"
	RelStream       = "http://vaemendis.net/opds-pse/stream"
	RelThumb        = "http://opds-spec.org/image/thumbnail"
	RelAcquisition  = "http://opds-spec.org/acquisition"
	RelAlternate    = "alternate"
	RelSubsection   = "subsection"
	RWPMContext     = "https://readium.org/webpub-manifest/context.jsonld"
	VocabularyIRI   = "https://github.com/n1atsys0r/PandaOPDS/vocab#"
)

type SubjectTranslator interface {
	DisplayTag(namespace, key string) (string, bool)
	DisplayCategory(category string) string
}

type Builder struct {
	Settings   config.Settings
	Translator SubjectTranslator
}

func (b Builder) Href(path string) string {
	if b.Settings.PublicBaseURL != "" && strings.HasPrefix(path, "/") {
		return b.Settings.PublicBaseURL + path
	}
	return path
}
func (b Builder) Upstream(gid int, token string) string {
	return fmt.Sprintf("https://%s/g/%d/%s/", b.Settings.SiteHost(), gid, token)
}
func iso(raw string) string {
	if raw != "" {
		if t, e := time.Parse(time.RFC3339, raw); e == nil {
			return t.UTC().Format("2006-01-02T15:04:05Z")
		}
		for _, layout := range []string{"2006-01-02 15:04", "2 January 2006, 15:04", "2 Jan 2006, 15:04"} {
			if t, e := time.ParseInLocation(layout, raw, time.UTC); e == nil {
				return t.Format("2006-01-02T15:04:05Z")
			}
		}
	}
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}

type AtomLink struct {
	Rel        string `xml:"rel,attr"`
	Href       string `xml:"href,attr"`
	Type       string `xml:"type,attr,omitempty"`
	Title      string `xml:"title,attr,omitempty"`
	Count      *int   `xml:"pse:count,attr,omitempty"`
	FacetGroup string `xml:"opds:facetGroup,attr,omitempty"`
	Active     *bool  `xml:"opds:activeFacet,attr,omitempty"`
}
type AtomCategory struct {
	Term   string `xml:"term,attr"`
	Label  string `xml:"label,attr,omitempty"`
	Scheme string `xml:"scheme,attr,omitempty"`
}
type AtomText struct {
	Type  string `xml:"type,attr,omitempty"`
	Value string `xml:",chardata"`
}
type AtomAuthor struct {
	Name string `xml:"name"`
}
type AtomEntry struct {
	ID       string        `xml:"id"`
	Title    string        `xml:"title"`
	Updated  string        `xml:"updated"`
	Author   *AtomAuthor   `xml:"author,omitempty"`
	Category *AtomCategory `xml:"category,omitempty"`
	Summary  *AtomText     `xml:"summary,omitempty"`
	Links    []AtomLink    `xml:"link,omitempty"`
}
type AtomFeed struct {
	XMLName   xml.Name    `xml:"feed"`
	DefaultNS string      `xml:"xmlns,attr"`
	PSE       string      `xml:"xmlns:pse,attr"`
	OPDS      string      `xml:"xmlns:opds,attr"`
	SearchNS  string      `xml:"xmlns:opensearch,attr"`
	Title     string      `xml:"title"`
	ID        string      `xml:"id"`
	Updated   string      `xml:"updated"`
	Author    AtomAuthor  `xml:"author"`
	Links     []AtomLink  `xml:"link"`
	Entries   []AtomEntry `xml:"entry"`
}

func (b Builder) atomFeed(title, updated string, entries []AtomEntry, more []AtomLink) string {
	f := AtomFeed{DefaultNS: "http://www.w3.org/2005/Atom", PSE: "http://vaemendis.net/opds-pse/ns", OPDS: "http://opds-spec.org/2010/catalog", SearchNS: "http://a9.com/-/spec/opensearch/1.1/", Title: title, ID: "urn:ehentai:" + title, Updated: updated, Author: AtomAuthor{Name: "PandaOPDS"}, Entries: entries}
	f.Links = []AtomLink{{Rel: "self", Href: b.Href("/opds/v1.2"), Type: MIMEAtomNav, Title: "PandaOPDS"}, {Rel: "start", Href: b.Href("/opds/v1.2"), Type: MIMEAtomNav, Title: "PandaOPDS"}, {Rel: "search", Href: b.Href("/opds/v1.2/search.xml"), Type: MIMEOpenSearch, Title: "Search"}}
	f.Links = append(f.Links, more...)
	data, _ := xml.MarshalIndent(f, "", "  ")
	return xml.Header + string(data) + "\n"
}
func (b Builder) RootAtom(nav [][3]string) string {
	now := iso("")
	entries := []AtomEntry{}
	for _, n := range nav {
		entries = append(entries, AtomEntry{ID: "urn:ehentai:subsection:" + strings.ToLower(n[0]), Title: n[0], Updated: now, Summary: &AtomText{Type: "text", Value: n[2]}, Links: []AtomLink{{Rel: RelSubsection, Href: b.Href(n[1]), Type: MIMEAtomAcq}}})
	}
	return b.atomFeed("PandaOPDS", now, entries, nil)
}
func (b Builder) OpenSearch(path string) string {
	template := b.Href(path + "?query={searchTerms}")
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<OpenSearchDescription xmlns="http://a9.com/-/spec/opensearch/1.1/"><ShortName>E-Hentai</ShortName><Description>Search E-Hentai galleries via PandaOPDS</Description><InputEncoding>UTF-8</InputEncoding><Url type="%s" template="%s"/></OpenSearchDescription>`, html.EscapeString(MIMEAtomAcq), html.EscapeString(template))
}

func (b Builder) AtomList(items []eh.GalleryListItem, title, feedID, query string, next string, facets [][3]string) string {
	entries := make([]AtomEntry, 0, len(items))
	for _, item := range items {
		clean, authors := parseTitle(item.Title)
		entry := AtomEntry{ID: fmt.Sprintf("urn:ehentai:gallery:%d:%s", item.GID, item.Token), Title: clean, Updated: iso(item.PublishTime), Category: &AtomCategory{Term: item.Category, Label: item.Category, Scheme: "http://e-hentai.org"}}
		if len(authors) > 0 {
			entry.Author = &AtomAuthor{Name: strings.Join(authors, ", ")}
		}
		links := []AtomLink{{Rel: RelThumb, Href: b.Href(fmt.Sprintf("/image/%d/%s/thumb", item.GID, item.Token)), Type: "image/jpeg"}, {Rel: RelAcquisition, Href: b.Href(fmt.Sprintf("/opds/v1.2/gallery/%d/%s/chapters", item.GID, item.Token)), Type: MIMEAtomAcq}}
		if item.PageCount != nil && *item.PageCount > 0 {
			count := *item.PageCount
			links = append(links, AtomLink{Rel: RelStream, Href: b.Href(fmt.Sprintf("/stream/%d/%s/page/{pageNumber}", item.GID, item.Token)), Type: MIMEImage, Count: &count})
		}
		links = append(links, AtomLink{Rel: RelAlternate, Href: b.Upstream(item.GID, item.Token), Type: "text/html", Title: b.Settings.SiteHost()})
		entry.Links = links
		entries = append(entries, entry)
	}
	more := []AtomLink{}
	if next != "" {
		more = append(more, AtomLink{Rel: "next", Href: next, Type: MIMEAtomAcq, Title: "Next page"})
	}
	for _, f := range facets {
		active := f[2] == "true"
		more = append(more, AtomLink{Rel: "http://opds-spec.org/facet", Href: f[1], Type: MIMEAtomAcq, Title: f[0], FacetGroup: "period", Active: &active})
	}
	_ = feedID
	_ = query
	return b.atomFeed(title, iso(""), entries, more)
}
func (b Builder) AtomChapter(gid int, token, title, updated, author, category string, count int) string {
	links := []AtomLink{{Rel: RelThumb, Href: b.Href(fmt.Sprintf("/image/%d/%s/thumb", gid, token)), Type: "image/jpeg"}, {Rel: RelStream, Href: b.Href(fmt.Sprintf("/stream/%d/%s/page/{pageNumber}", gid, token)), Type: MIMEImage, Count: &count}, {Rel: RelAlternate, Href: b.Upstream(gid, token), Type: "text/html", Title: b.Settings.SiteHost()}}
	e := AtomEntry{ID: fmt.Sprintf("urn:ehentai:gallery:%d:%s", gid, token), Title: "Chapter 1: " + title, Updated: iso(updated), Links: links}
	if author != "" {
		e.Author = &AtomAuthor{Name: author}
	}
	if category != "" {
		e.Category = &AtomCategory{Term: category, Label: category, Scheme: "http://e-hentai.org"}
	}
	return b.atomFeed(title, iso(""), []AtomEntry{e}, nil)
}

func (b Builder) Publication(item eh.GalleryListItem, detail bool) map[string]any {
	title, authors := parseTitle(item.Title)
	subjects := b.Subjects(item.Tags, item.Category, false)
	extra := map[string]any{}
	if item.Rating > 0 {
		extra["x:rating"] = item.Rating
	}
	if item.Uploader != "" {
		extra["x:uploader"] = item.Uploader
	}
	count := 0
	if item.PageCount != nil {
		count = *item.PageCount
	}
	return b.publication(item.GID, item.Token, title, iso(item.PublishTime), authors, item.Language, count, subjects, extra, detail)
}
func (b Builder) publication(gid int, token, title, modified string, authors []string, language string, count int, subjects []map[string]any, extra map[string]any, detail bool) map[string]any {
	identifier := fmt.Sprintf("urn:ehentai:gallery:%d:%s", gid, token)
	md := map[string]any{"title": title, "identifier": identifier, "modified": modified}
	if len(authors) > 0 {
		aa := []map[string]string{}
		for _, a := range authors {
			aa = append(aa, map[string]string{"name": a})
		}
		md["authors"] = aa
		md["author"] = aa
	}
	if language != "" {
		md["language"] = []string{language}
	}
	if modified != "" {
		md["published"] = modified
	}
	if len(subjects) > 0 {
		md["subject"] = subjects
	}
	if count > 0 {
		md["numberOfPages"] = count
	}
	for k, v := range extra {
		md[k] = v
	}
	links := []map[string]any{}
	stream := b.Href(fmt.Sprintf("/stream/%d/%s/page/{pageNumber}", gid, token))
	props := map[string]any(nil)
	if count > 0 {
		props = map[string]any{"numberOfItems": count}
	}
	direct := detail || !b.Settings.OPDSAcqDetail
	if direct {
		if count > 0 {
			links = append(links, b.Link(RelAcquisition, stream, MIMEImage, title, props), b.Link(RelStream, stream, MIMEImage, "", props))
		}
	} else {
		links = append(links, b.Link(RelAcquisition, b.Href(fmt.Sprintf("/opds/v2.0/gallery/%d/%s", gid, token)), MIMEOPDSAcq, title, props))
		if count > 0 {
			links = append(links, b.Link(RelStream, stream, MIMEImage, "", props))
		}
	}
	links = append(links, b.Link("self", b.Href(fmt.Sprintf("/opds/v2.0/gallery/%d/%s/publication", gid, token)), MIMEPublication, title, nil), b.Link(RelAlternate, b.Upstream(gid, token), "text/html", b.Settings.SiteHost(), nil))
	pub := map[string]any{"context": []any{RWPMContext, map[string]string{"x": VocabularyIRI}}, "metadata": md, "links": links, "images": []map[string]string{{"href": b.Href(fmt.Sprintf("/image/%d/%s/thumb", gid, token)), "type": "image/jpeg"}}}
	if detail && count > 0 {
		reading := []map[string]any{}
		for p := b.Settings.PSEPageBase; p < b.Settings.PSEPageBase+count; p++ {
			reading = append(reading, map[string]any{"href": b.Href(fmt.Sprintf("/stream/%d/%s/page/%d", gid, token, p)), "type": MIMEImage, "properties": map[string]any{"x:thumbnail": map[string]string{"href": b.Href(fmt.Sprintf("/image/%d/%s/page/%d/thumb", gid, token, p)), "type": "image/jpeg"}}})
		}
		pub["readingOrder"] = reading
	}
	return pub
}
func (b Builder) Link(rel, href, typ, title string, props map[string]any) map[string]any {
	m := map[string]any{"rel": rel, "href": href}
	if typ != "" {
		m["type"] = typ
	}
	if title != "" {
		m["title"] = title
	}
	if props != nil {
		m["properties"] = props
	}
	if strings.Contains(href, "{") {
		m["templated"] = true
	}
	return m
}
func (b Builder) NavigationDocument(nav []map[string]any, groups []map[string]any) map[string]any {
	doc := map[string]any{"metadata": map[string]any{"title": "PandaOPDS", "identifier": "urn:ehentai:root", "modified": iso("")}, "links": []map[string]any{b.Link("self", b.Href("/opds/v2.0"), MIMEOPDSNav, "PandaOPDS", nil), b.Link("start", b.Href("/opds/v2.0"), MIMEOPDSNav, "PandaOPDS", nil), b.Link("search", b.Href("/opds/v2.0/gallery?query={searchTerms}"), MIMEOPDSAcq, "Search", nil)}, "navigation": nav}
	if len(groups) > 0 {
		doc["groups"] = groups
	}
	return doc
}
func (b Builder) Acquisition(title, identifier, self string, publications []map[string]any, next string, facets []map[string]any) map[string]any {
	links := []map[string]any{b.Link("self", b.Href(self), MIMEOPDSAcq, title, nil), b.Link("start", b.Href("/opds/v2.0"), MIMEOPDSNav, "PandaOPDS", nil), b.Link("search", b.Href("/opds/v2.0/gallery?query={searchTerms}"), MIMEOPDSAcq, "Search", nil)}
	if next != "" {
		links = append(links, b.Link("next", next, MIMEOPDSAcq, "Next page", nil))
	}
	doc := map[string]any{"metadata": map[string]any{"title": title, "identifier": identifier, "modified": iso("")}, "links": links, "publications": publications}
	if len(facets) > 0 {
		doc["facets"] = facets
	}
	return doc
}
func (b Builder) CategoryFacets() []map[string]any {
	links := []map[string]any{{"href": b.Href("/opds/v2.0/gallery"), "title": "All"}}
	for _, f := range b.Settings.Facets {
		links = append(links, map[string]any{"href": b.Href("/opds/v2.0/gallery?category=" + url.QueryEscape(f.Name)), "title": f.Name})
	}
	return []map[string]any{{"metadata": map[string]string{"title": "Category"}, "links": links}}
}
func (b Builder) JSON(doc any) []byte {
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(doc)
	return out.Bytes()
}

var bracketRE = regexp.MustCompile(`\[[^\]]*\]|\([^)]*\)`)
var authorRE = regexp.MustCompile(`\[([^\]]+)\]\s*$`)

func parseTitle(title string) (string, []string) {
	cleanRaw := bracketRE.ReplaceAllString(title, "")
	cleanRaw = strings.Join(strings.Fields(cleanRaw), " ")
	pos := strings.Index(title, cleanRaw)
	if cleanRaw == "" || pos <= 0 {
		return strings.TrimSpace(cleanRaw), nil
	}
	m := authorRE.FindStringSubmatch(title[:pos])
	if len(m) < 2 || strings.TrimSpace(m[1]) == "" {
		return strings.TrimSpace(cleanRaw), nil
	}
	return strings.TrimSpace(cleanRaw), []string{strings.TrimSpace(m[1])}
}
func parseDetailTitle(title, jpn string) (string, []string) {
	if jpn != "" && strings.TrimSpace(bracketRE.ReplaceAllString(jpn, "")) != "" {
		return parseTitle(jpn)
	}
	return parseTitle(title)
}
func (b Builder) Subjects(tags []eh.GalleryTag, category string, categoryFirst bool) []map[string]any {
	tags = eh.FilterTagStatus(tags, b.Settings.TagStatusFilter)
	order := map[string]int{"language": 0, "parody": 1, "character": 2, "cosplayer": 3, "group": 4, "artist": 5, "female": 6, "male": 7, "mixed": 8, "location": 9, "other": 10}
	sort.SliceStable(tags, func(i, j int) bool {
		a, aok := order[tags[i].Namespace]
		c, cok := order[tags[j].Namespace]
		if !aok {
			a = 11
		}
		if !cok {
			c = 11
		}
		if a == c {
			return tags[i].Namespace+":"+tags[i].Key < tags[j].Namespace+":"+tags[j].Key
		}
		return a < c
	})
	out := []map[string]any{}
	seen := map[string]bool{}
	styled := 0
	for _, t := range tags {
		name := t.String()
		if b.Translator != nil {
			if translated, ok := b.Translator.DisplayTag(t.Namespace, t.Key); ok {
				name = translated
			}
		}
		if !seen[name] {
			m := map[string]any{"name": name}
			if len(t.Style) > 0 {
				m["x:style"] = t.Style
				styled = len(out) + 1
			}
			out = append(out, m)
			seen[name] = true
		}
	}
	if category != "" {
		name := "category:" + category
		if b.Translator != nil {
			name = b.Translator.DisplayCategory(category)
		}
		index := styled
		if categoryFirst {
			index = 0
		}
		found := -1
		for i, v := range out {
			if v["name"] == name {
				found = i
				break
			}
		}
		entry := map[string]any{"name": name}
		if found >= 0 {
			entry = out[found]
			out = append(out[:found], out[found+1:]...)
		}
		if index > len(out) {
			index = len(out)
		}
		out = append(out, nil)
		copy(out[index+1:], out[index:])
		out[index] = entry
	}
	return out
}

func ParseDetailTitle(title, jpn string) (string, []string) {
	if jpn != "" && strings.TrimSpace(bracketRE.ReplaceAllString(jpn, "")) != "" {
		return parseTitle(jpn)
	}
	return parseTitle(title)
}
func FormatDate(raw string) string { return iso(raw) }
func BuildSubjects(tags []eh.GalleryTag, category string, categoryFirst bool) []map[string]any {
	return (Builder{}).Subjects(tags, category, categoryFirst)
}
func (b Builder) DetailLinks(gid int, token, title string, count int) []map[string]any {
	props := map[string]any(nil)
	if count > 0 {
		props = map[string]any{"numberOfItems": count}
	}
	stream := b.Href(fmt.Sprintf("/stream/%d/%s/page/{pageNumber}", gid, token))
	links := []map[string]any{}
	if count > 0 {
		links = append(links, b.Link(RelAcquisition, stream, MIMEImage, title, props), b.Link(RelStream, stream, MIMEImage, "", props))
	}
	links = append(links, b.Link("self", b.Href(fmt.Sprintf("/opds/v2.0/gallery/%d/%s/publication", gid, token)), MIMEPublication, title, nil), b.Link(RelAlternate, b.Upstream(gid, token), "text/html", b.Settings.SiteHost(), nil))
	return links
}
func (b Builder) ReadingOrder(gid int, token string, count int) []map[string]any {
	out := []map[string]any{}
	for p := b.Settings.PSEPageBase; p < b.Settings.PSEPageBase+count; p++ {
		out = append(out, map[string]any{"href": b.Href(fmt.Sprintf("/stream/%d/%s/page/%d", gid, token, p)), "type": MIMEImage, "properties": map[string]any{"x:thumbnail": map[string]string{"href": b.Href(fmt.Sprintf("/image/%d/%s/page/%d/thumb", gid, token, p)), "type": "image/jpeg"}}})
	}
	return out
}
