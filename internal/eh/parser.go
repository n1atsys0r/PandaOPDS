package eh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"
)

var (
	galleryHrefRE = regexp.MustCompile(`/g/(\d+)/([0-9a-fA-F]+)/`)
	streamHrefRE  = regexp.MustCompile(`/s/([^/]+)/(\d+)-(\d+)$`)
	showingRE     = regexp.MustCompile(`Showing\s+(\d+)\s*-\s*(\d+)\s+of\s+(\d+)\s+images`)
	urlRE         = regexp.MustCompile(`url\(["']?(.+?)["']?\)`)
	favcatPosRE   = regexp.MustCompile(`(?i)background-position\s*:\s*0px\s*-\s*(\d+)px`)
	pageCursorRE  = regexp.MustCompile(`(?:next|prev)=([\d-]+)`)
	pageLinkRE    = regexp.MustCompile(`(?:p|page)=(\d+)`)
	pageNumRE     = regexp.MustCompile(`(\d+)\s+pages?`)
	img509        = map[string]bool{"https://ehgt.org/g/509.gif": true, "https://exhentai.org/img/509.gif": true}
)

func parseDoc(src string) (*xhtml.Node, error) { return xhtml.Parse(strings.NewReader(src)) }
func attr(n *xhtml.Node, key string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attr {
		if a.Key == key {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}
func classHas(n *xhtml.Node, name string) bool {
	for _, c := range strings.Fields(attr(n, "class")) {
		if c == name {
			return true
		}
	}
	return false
}
func hasID(n *xhtml.Node, id string) bool { return n != nil && attr(n, "id") == id }
func walk(n *xhtml.Node, fn func(*xhtml.Node)) {
	if n == nil {
		return
	}
	fn(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, fn)
	}
}
func all(n *xhtml.Node, pred func(*xhtml.Node) bool) []*xhtml.Node {
	out := []*xhtml.Node{}
	walk(n, func(x *xhtml.Node) {
		if pred(x) {
			out = append(out, x)
		}
	})
	return out
}
func first(n *xhtml.Node, pred func(*xhtml.Node) bool) *xhtml.Node {
	if n == nil {
		return nil
	}
	if pred(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if x := first(c, pred); x != nil {
			return x
		}
	}
	return nil
}
func nodeText(n *xhtml.Node) string {
	var b strings.Builder
	walk(n, func(x *xhtml.Node) {
		if x.Type == xhtml.TextNode {
			b.WriteString(x.Data)
			b.WriteByte(' ')
		}
	})
	return strings.Join(strings.Fields(b.String()), " ")
}
func nodeOuterHTML(n *xhtml.Node) string {
	if n == nil {
		return ""
	}
	var b bytes.Buffer
	_ = xhtml.Render(&b, n)
	return b.String()
}
func absURL(value, host string) string {
	if strings.HasPrefix(value, "//") {
		return "https:" + value
	}
	if strings.HasPrefix(value, "/") {
		return "https://" + host + value
	}
	return value
}
func parseGalleryURL(href string) (int, string, bool) {
	m := galleryHrefRE.FindStringSubmatch(href)
	if len(m) != 3 {
		return 0, "", false
	}
	n, e := strconv.Atoi(m[1])
	return n, m[2], e == nil
}
func parseStyle(style string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(style, ";") {
		k, v, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "!important"))
		if k == "color" || k == "border-color" || k == "background" {
			if k == "border-color" {
				k = "borderColor"
			}
			out[k] = v
		}
	}
	return out
}
func tagFrom(n *xhtml.Node, title string) GalleryTag {
	pieces := strings.SplitN(title, ":", 2)
	ns, key := "temp", strings.TrimSpace(title)
	if len(pieces) == 2 && strings.TrimSpace(pieces[0]) != "" {
		ns, key = strings.TrimSpace(pieces[0]), strings.TrimSpace(pieces[1])
	}
	status := "confidence"
	if classHas(n, "gtl") {
		status = "skepticism"
	} else if classHas(n, "gtw") {
		status = "incorrect"
	}
	st := parseStyle(attr(n, "style"))
	var style map[string]string
	if len(st) > 0 {
		style = map[string]string{}
		for k, v := range st {
			switch k {
			default:
				style[k] = v
			}
		}
	}
	return GalleryTag{Namespace: ns, Key: key, Status: status, Style: style}
}

func ParseListPage(src string) (GalleryPageInfo, error) {
	doc, err := parseDoc(src)
	if err != nil {
		return GalleryPageInfo{}, err
	}
	out := GalleryPageInfo{Galleries: []GalleryListItem{}, FavcatMap: ParseFavoritesCategories(src)}
	// EH list page row classes vary by layout; collect visible gallery anchors
	// and de-duplicate by gid while using the nearest gallery row container.
	for _, a := range all(doc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "a" && attr(n, "href") != "" }) {
		gid, token, ok := parseGalleryURL(attr(a, "href"))
		if !ok {
			continue
		}
		row, view, knownLayout := knownListRow(a)
		if !knownLayout {
			row = a
			for p := 0; p < 8 && row.Parent != nil; p++ {
				row = row.Parent
				if row.Type == xhtml.ElementNode && (classHas(row, "gl1e") || classHas(row, "gl1t") || classHas(row, "gl1c") || classHas(row, "gl1m") || classHas(row, "gl1")) {
					break
				}
			}
		}
		if row == nil {
			continue
		}
		if duplicateGID(out.Galleries, gid) {
			continue
		}
		item := GalleryListItem{GID: gid, Token: token, Title: "", Category: "", Tags: []GalleryTag{}}
		if title := first(row, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && classHas(n, "glink") }); title != nil {
			item.Title = nodeText(title)
		} else {
			item.Title = nodeText(a)
		}
		cat := first(row, func(n *xhtml.Node) bool {
			return n.Type == xhtml.ElementNode && (classHas(n, "cs") || classHas(n, "cn"))
		})
		if cat != nil {
			item.Category = nodeText(cat)
		}
		for _, im := range all(row, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "img" }) {
			item.CoverURL = attr(im, "data-src")
			if item.CoverURL == "" {
				item.CoverURL = attr(im, "src")
			}
			if item.CoverURL != "" {
				break
			}
		}
		if item.CoverURL == "" {
			if bg := first(row, func(n *xhtml.Node) bool {
				return n.Type == xhtml.ElementNode && strings.Contains(attr(n, "style"), "url(")
			}); bg != nil {
				if m := urlRE.FindStringSubmatch(attr(bg, "style")); len(m) > 1 {
					item.CoverURL = absURL(m[1], "ehgt.org")
				}
			}
		}
		if m := pageNumRE.FindStringSubmatch(nodeText(row)); len(m) > 1 {
			if n, e := strconv.Atoi(m[1]); e == nil {
				item.PageCount = &n
			}
		}
		if s := first(row, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && classHas(n, "ir") }); s != nil {
			offs := regexp.MustCompile(`-?\d+px`).FindAllString(attr(s, "style"), -1)
			if len(offs) >= 2 {
				x, _ := strconv.Atoi(strings.TrimSuffix(offs[0], "px"))
				y, _ := strconv.Atoi(strings.TrimSuffix(offs[1], "px"))
				item.Rating = math.Max(0, 5-float64(-x)/16-float64(boolInt(y == -21))*.5)
			}
		}
		tagNodes := all(row, func(n *xhtml.Node) bool {
			return n.Type == xhtml.ElementNode && attr(n, "title") != "" && (classHas(n, "gt") || classHas(n, "gtl") || classHas(n, "gtw"))
		})
		if knownLayout && view == "extended" {
			if name := first(row, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && classHas(n, "glname") }); name != nil {
				children := childrenNamed(name, "div")
				if len(children) > 1 {
					scoped := all(children[1], func(n *xhtml.Node) bool {
						return n.Type == xhtml.ElementNode && n.Data == "div" && attr(n, "title") != ""
					})
					if len(scoped) > 0 {
						tagNodes = scoped
					}
				}
			}
		}
		for _, t := range tagNodes {
			item.Tags = append(item.Tags, tagFrom(t, attr(t, "title")))
		}
		item.Language = languageFromTags(item.Tags)
		for _, link := range all(row, func(n *xhtml.Node) bool {
			return n.Type == xhtml.ElementNode && n.Data == "a" && strings.Contains(attr(n, "href"), "/uploader/")
		}) {
			item.Uploader = nodeText(link)
			break
		}
		if u := first(row, func(n *xhtml.Node) bool {
			return n.Type == xhtml.ElementNode && (attr(n, "id") == "posted" || strings.Contains(attr(n, "id"), "posted"))
		}); u != nil {
			item.PublishTime = nodeText(u)
		}
		if len(out.FavcatMap) > 0 {
			if u := first(row, func(n *xhtml.Node) bool {
				return n.Type == xhtml.ElementNode && (attr(n, "id") == "posted" || strings.Contains(attr(n, "id"), "posted"))
			}); u != nil {
				title := attr(u, "title")
				for id, name := range out.FavcatMap {
					if name == title {
						x := id
						item.Favcat = &x
						break
					}
				}
			}
		}
		checkExpunged := row
		if knownLayout {
			checkExpunged = first(row, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && classHas(n, "glink") })
		}
		if first(checkExpunged, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "s" }) != nil {
			item.Expunged = true
		}
		out.Galleries = append(out.Galleries, item)
	}
	for _, n := range all(doc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && attr(n, "id") == "unext" }) {
		if m := pageCursorRE.FindStringSubmatch(attr(n, "href")); len(m) > 1 {
			out.NextGID = m[1]
		}
		break
	}
	for _, n := range all(doc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && attr(n, "id") == "uprev" }) {
		if m := regexp.MustCompile(`prev=([\d-]+)`).FindStringSubmatch(attr(n, "href")); len(m) > 1 {
			out.PrevGID = m[1]
		}
		break
	}
	if n := first(doc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && classHas(n, "searchtext") }); n != nil {
		txt := nodeText(n)
		if !strings.Contains(txt, "hundreds") && !strings.Contains(txt, "thousands") {
			m := regexp.MustCompile(`[\d,]+\+?`).FindString(txt)
			m = strings.TrimSuffix(strings.ReplaceAll(m, ",", ""), "+")
			if c, e := strconv.Atoi(m); e == nil {
				out.TotalCount = &c
			}
		}
	}
	// Ranklists use a `?p=` next link in the final cell of .ptt.
	if out.NextGID == "" {
		tr := first(doc, func(n *xhtml.Node) bool {
			if n.Type != xhtml.ElementNode || n.Data != "tr" {
				return false
			}
			for parent := n.Parent; parent != nil; parent = parent.Parent {
				if parent.Type == xhtml.ElementNode && classHas(parent, "ptt") {
					return true
				}
			}
			return false
		})
		if tr != nil {
			tds := childrenNamed(tr, "td")
			if len(tds) > 0 {
				if a := first(tds[len(tds)-1], func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "a" }); a != nil {
					if m := pageLinkRE.FindStringSubmatch(attr(a, "href")); len(m) > 1 {
						if p, e := strconv.Atoi(m[1]); e == nil {
							p++
							out.NextPage = &p
						}
					}
				}
			}
		}
	}
	return out, nil
}

func knownListRow(anchor *xhtml.Node) (*xhtml.Node, string, bool) {
	for n := anchor; n != nil; n = n.Parent {
		if n.Type != xhtml.ElementNode || n.Data != "tr" {
			continue
		}
		p := n.Parent
		if p != nil && p.Type == xhtml.ElementNode && (p.Data == "tbody" || p.Data == "thead" || p.Data == "tfoot") {
			p = p.Parent
		}
		if p == nil || p.Type != xhtml.ElementNode || p.Data != "table" || !classHas(p, "itg") {
			continue
		}
		switch {
		case classHas(p, "gltc"):
			return n, "compact", true
		case classHas(p, "glte"):
			return n, "extended", true
		case classHas(p, "gltm"):
			return n, "minimal", true
		}
	}
	for p := anchor; p != nil; p = p.Parent {
		if p.Type != xhtml.ElementNode || p.Data != "table" || !classHas(p, "itg") || !classHas(p, "gld") {
			continue
		}
		row := anchor
		for row.Parent != nil && row.Parent != p {
			row = row.Parent
		}
		if row.Parent == p && row.Type == xhtml.ElementNode && row.Data == "div" {
			return row, "thumbnail", true
		}
	}
	return nil, "", false
}

func duplicateGID(items []GalleryListItem, gid int) bool {
	for _, x := range items {
		if x.GID == gid {
			return true
		}
	}
	return false
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func childrenNamed(n *xhtml.Node, tag string) []*xhtml.Node {
	var out []*xhtml.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode && c.Data == tag {
			out = append(out, c)
		}
	}
	return out
}

func ParseDetailPage(src, siteHost string, pageIndex int) (DetailPageInfo, error) {
	doc, err := parseDoc(src)
	if err != nil {
		return DetailPageInfo{}, err
	}
	gdt := first(doc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && attr(n, "id") == "gdt" })
	if gdt == nil {
		return DetailPageInfo{}, fmt.Errorf("detail page has no #gdt block")
	}
	out := DetailPageInfo{CurrentPageNo: 1, Thumbnails: []Thumbnail{}, Tags: []GalleryTag{}, Comments: []GalleryComment{}, NewerVersions: []GalleryVersion{}}
	out.Favcat, out.FavcatName = parseDetailFavorite(findID(doc, "gdf"))
	for i, a := range all(gdt, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "a" }) {
		href := attr(a, "href")
		div := first(a, func(n *xhtml.Node) bool {
			return n.Type == xhtml.ElementNode && n.Data == "div" && attr(n, "style") != ""
		})
		style := attr(div, "style")
		thumb := ""
		var thumbImage *xhtml.Node
		urlEnd := 0
		if m := urlRE.FindStringSubmatchIndex(style); len(m) >= 4 {
			thumb = absURL(style[m[2]:m[3]], siteHost)
			urlEnd = m[1]
		}
		if thumb == "" {
			if im := first(a, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "img" }); im != nil {
				thumbImage = im
				thumb = absURL(attr(im, "src"), siteHost)
			}
		}
		if href == "" || thumb == "" {
			continue
		}
		pno := pageIndex*20 + i + 1
		if m := streamHrefRE.FindStringSubmatch(href); len(m) > 3 {
			pno, _ = strconv.Atoi(m[3])
		} else if m := regexp.MustCompile(`/mpv/(\d+)/`).FindStringSubmatch(href); len(m) > 0 {
			hash := attr(div, "data-orghash")
			if len(hash) >= 10 {
				href = fmt.Sprintf("/s/%s/%s-%d", hash[:10], m[1], pno)
			}
		}
		w, h := styleDimension(style, "width"), styleDimension(style, "height")
		if thumbImage != nil {
			if w == 0 {
				w = attrDimension(thumbImage, "width")
			}
			if h == 0 {
				h = attrDimension(thumbImage, "height")
			}
		}
		bx, by := backgroundPosition(style, urlEnd)
		large := hasClassAncestor(a, gdt, "gdtl") || (div == nil && w == 0 && h == 0)
		item := Thumbnail{Href: href, ThumbURL: thumb, PageNo: pno, Width: w, Height: h, OriginHash: attr(div, "data-orghash"), BackgroundX: bx, BackgroundY: by, IsLarge: large}
		if large {
			v := false
			item.IsSprite = &v
		}
		out.Thumbnails = append(out.Thumbnails, item)
	}
	if len(out.Thumbnails) == 0 {
		return DetailPageInfo{}, fmt.Errorf("detail page has no image thumbnails")
	}
	classifyThumbnails(out.Thumbnails)
	if n := first(doc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && classHas(n, "gpc") }); n != nil {
		if m := showingRE.FindStringSubmatch(nodeText(n)); len(m) > 3 {
			out.ImageNoFrom, _ = strconv.Atoi(m[1])
			out.ImageNoFrom--
			out.ImageNoTo, _ = strconv.Atoi(m[2])
			out.ImageNoTo--
			out.ImageCount, _ = strconv.Atoi(m[3])
		}
	}
	// E-Hentai page number appears in `.ptds > a`; thumbnails always default 1-based.
	if n := first(doc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && classHas(n, "ptds") }); n != nil {
		if p, e := strconv.Atoi(nodeText(n)); e == nil && p > 0 {
			out.CurrentPageNo = p
		}
	}
	if tr := first(doc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "tr" && classHas(n, "ptt") }); tr != nil {
		tds := childrenNamed(tr, "td")
		if len(tds) >= 2 {
			if a := first(tds[len(tds)-2], func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "a" }); a != nil {
				if p, e := strconv.Atoi(nodeText(a)); e == nil {
					out.PageCount = p
				}
			}
		}
	}
	out.Title = nodeText(findID(doc, "gn"))
	out.TitleJPN = nodeText(findID(doc, "gj"))
	if n := first(doc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && attr(n, "id") == "gdc" }); n != nil {
		if c := first(n, func(n *xhtml.Node) bool { return classHas(n, "cs") }); c != nil {
			out.Category = nodeText(c)
		}
	}
	if n := first(doc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && attr(n, "id") == "gd1" }); n != nil {
		if d := first(n, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && attr(n, "style") != "" }); d != nil {
			if m := urlRE.FindStringSubmatch(attr(d, "style")); len(m) > 1 {
				out.CoverURL = absURL(m[1], siteHost)
			}
		}
	}
	if n := first(doc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && attr(n, "id") == "gdn" }); n != nil {
		if a := first(n, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "a" }); a != nil {
			out.Uploader = nodeText(a)
		}
	}
	if n := first(doc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && attr(n, "id") == "gdd" }); n != nil {
		rows := all(n, func(x *xhtml.Node) bool { return x.Type == xhtml.ElementNode && x.Data == "tr" })
		for _, r := range rows {
			cells := childrenNamed(r, "td")
			if len(cells) < 2 {
				continue
			}
			key := strings.TrimSuffix(nodeText(cells[0]), ":")
			value := nodeText(cells[1])
			switch key {
			case "Posted":
				out.PublishTime = value
			case "Language":
				out.Language = mapLanguage(strings.TrimSuffix(strings.ToLower(strings.Join(strings.Fields(value), " ")), " tr"))
			case "File Size":
				out.FilesizeText = value
			}
			if strings.Contains(value, "Expunged") {
				out.Expunged = true
			}
		}
	}
	if n := findID(doc, "gd5"); n != nil {
		if m := regexp.MustCompile(`Torrent[^)]*\((\d+)\)`).FindStringSubmatch(nodeText(n)); len(m) > 1 {
			out.TorrentCount, _ = strconv.Atoi(m[1])
		}
	}
	if n := findID(doc, "rating_image"); n != nil {
		if m := first(n, func(x *xhtml.Node) bool { return classHas(x, "ir") }); m != nil {
			offs := regexp.MustCompile(`-?\d+px`).FindAllString(attr(m, "style"), -1)
			if len(offs) >= 2 {
				x, _ := strconv.Atoi(strings.TrimSuffix(offs[0], "px"))
				y, _ := strconv.Atoi(strings.TrimSuffix(offs[1], "px"))
				out.Rating = math.Max(0, 5-float64(-x)/16-float64(boolInt(y == -21))*.5)
			}
		}
	}
	if label := findID(doc, "rating_label"); label != nil {
		text := nodeText(label)
		if m := regexp.MustCompile(`(?i)(?:average|score|rating)?\s*:\s*(\d+(?:\.\d+)?)`).FindStringSubmatch(text); len(m) > 1 {
			if r, err := strconv.ParseFloat(m[1], 64); err == nil && r > 0 {
				out.Rating = r
			}
		} else if strings.EqualFold(strings.TrimSpace(text), "Not Yet Rated") {
			out.Rating = 0
		}
	} else if gdr := findID(doc, "gdr"); gdr != nil {
		text := nodeText(gdr)
		if m := regexp.MustCompile(`(?i)(?:average|score|rating)?\s*:\s*(\d+(?:\.\d+)?)`).FindStringSubmatch(text); len(m) > 1 {
			if r, err := strconv.ParseFloat(m[1], 64); err == nil && r > 0 {
				out.Rating = r
			}
		}
	}
	if m := regexp.MustCompile(`(?i)average_rating\s*=\s*(\d+(?:\.\d+)?)`).FindStringSubmatch(src); len(m) > 1 {
		if r, err := strconv.ParseFloat(m[1], 64); err == nil && r > 0 {
			out.Rating = r
		}
	}
	for _, tag := range all(doc, func(n *xhtml.Node) bool {
		return n.Type == xhtml.ElementNode && strings.HasPrefix(attr(n, "id"), "td_")
	}) {
		id := strings.TrimPrefix(attr(tag, "id"), "td_")
		parts := strings.SplitN(id, ":", 2)
		ns, key := "temp", strings.ReplaceAll(id, "_", " ")
		if len(parts) == 2 {
			ns = parts[0]
			key = strings.ReplaceAll(parts[1], "_", " ")
		}
		if key != "" {
			out.Tags = append(out.Tags, tagFrom(tag, ns+":"+key))
		}
	}
	out.Comments = parseDetailComments(doc)
	gnd := findID(doc, "gnd")
	seenVersions := map[string]bool{}
	addedTimeRE := regexp.MustCompile(`\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}|\d{1,2}\s+[A-Za-z]+\s+\d{4},\s+\d{2}:\d{2}`)
	for _, a := range all(gnd, func(n *xhtml.Node) bool {
		return n.Type == xhtml.ElementNode && n.Data == "a" && strings.Contains(attr(n, "href"), "/g/")
	}) {
		gid, tok, ok := parseGalleryURL(attr(a, "href"))
		title := nodeText(a)
		if !ok || gid <= 0 || tok == "" || title == "" {
			continue
		}
		key := fmt.Sprintf("%d:%s", gid, tok)
		if seenVersions[key] {
			continue
		}
		seenVersions[key] = true
		version := GalleryVersion{GID: gid, Token: tok, Title: title}
		if a.NextSibling != nil && a.NextSibling.Type == xhtml.TextNode {
			for _, raw := range addedTimeRE.FindAllString(a.NextSibling.Data, -1) {
				if parsed := ParsePublishTime(raw); parsed != "" {
					version.AddedAt = parsed
					break
				}
			}
		}
		out.NewerVersions = append(out.NewerVersions, version)
	}
	return out, nil
}

func parseDetailFavorite(gdf *xhtml.Node) (*int, string) {
	if gdf == nil {
		return nil, ""
	}
	name := strings.TrimSpace(nodeText(gdf))
	if name == "" || strings.EqualFold(name, "Add to Favorites") {
		return nil, ""
	}
	for _, node := range all(gdf, func(n *xhtml.Node) bool {
		return n.Type == xhtml.ElementNode && attr(n, "style") != ""
	}) {
		style := attr(node, "style")
		if !strings.Contains(strings.ToLower(style), "/fav.png") {
			continue
		}
		match := favcatPosRE.FindStringSubmatch(style)
		if len(match) != 2 {
			continue
		}
		position, err := strconv.Atoi(match[1])
		if err != nil || position < 2 || (position-2)%19 != 0 {
			continue
		}
		slot := (position - 2) / 19
		if slot <= 9 {
			return &slot, name
		}
	}
	return nil, ""
}

func parseDetailComments(doc *xhtml.Node) []GalleryComment {
	cdiv := findID(doc, "cdiv")
	if cdiv == nil {
		return []GalleryComment{}
	}
	out := []GalleryComment{}
	showUserRE := regexp.MustCompile(`(?i)(?:\?|&)showuser=(\d+)`)
	commentIDRE := regexp.MustCompile(`comment_(\d+)`)
	postedRE := regexp.MustCompile(`(?i)Posted on\s+(\d+\s+[A-Za-z]+\s+\d{4},\s+\d{1,2}:\d{2})`)
	for _, row := range all(cdiv, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && classHas(n, "c1") }) {
		c3 := first(row, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && classHas(n, "c3") })
		c6 := first(row, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && classHas(n, "c6") })
		comment := GalleryComment{ContentHTML: renderHTML(c6)}
		if m := commentIDRE.FindStringSubmatch(attr(c6, "id")); len(m) > 1 {
			comment.ID, _ = strconv.Atoi(m[1])
		}
		anchors := all(c3, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "a" })
		if len(anchors) > 0 {
			comment.Username = nodeText(anchors[0])
		}
		for _, a := range anchors[1:] {
			if m := showUserRE.FindStringSubmatch(attr(a, "href")); len(m) > 1 {
				if id, err := strconv.Atoi(m[1]); err == nil {
					comment.UserID = &id
					break
				}
			}
		}
		if m := postedRE.FindStringSubmatch(nodeText(c3)); len(m) > 1 {
			if tm, err := time.ParseInLocation("2 January 2006, 15:04", m[1], time.Local); err == nil {
				comment.Time = tm.Format("2006-01-02 15:04")
			}
		}
		if c8 := first(row, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && classHas(n, "c8") }); c8 != nil {
			if strong := first(c8, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "strong" }); strong != nil {
				comment.LastEditTime = nodeText(strong)
			}
		}
		out = append(out, comment)
	}
	return out
}

func renderHTML(n *xhtml.Node) string {
	if n == nil {
		return ""
	}
	var b bytes.Buffer
	if xhtml.Render(&b, n) != nil {
		return ""
	}
	return b.String()
}

func attrDimension(n *xhtml.Node, name string) float64 {
	value := regexp.MustCompile(`\d+(?:\.\d+)?`).FindString(attr(n, name))
	if value == "" {
		return 0
	}
	v, _ := strconv.ParseFloat(value, 64)
	return v
}

func backgroundPosition(style string, urlEnd int) (*float64, *float64) {
	if urlEnd <= 0 || regexp.MustCompile(`(?i)\bbackground-size\s*:`).MatchString(style) || urlEnd > len(style) {
		return nil, nil
	}
	re := regexp.MustCompile(`(?i)\s+(-?(?:\d+(?:\.\d+)?px|0))(?:\s+(-?(?:\d+(?:\.\d+)?px|0)))?\s+no-repeat\b`)
	m := re.FindStringSubmatch(style[urlEnd:])
	if len(m) < 2 {
		return nil, nil
	}
	parse := func(v string) float64 {
		v = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(v)), "px")
		n, _ := strconv.ParseFloat(v, 64)
		return n
	}
	x := parse(m[1])
	y := 0.0
	if len(m) > 2 {
		y = parse(m[2])
	}
	return &x, &y
}

func hasClassAncestor(node, stop *xhtml.Node, class string) bool {
	for n := node; n != nil && n != stop; n = n.Parent {
		if classHas(n, class) {
			return true
		}
	}
	return false
}

func classifyThumbnails(thumbs []Thumbnail) {
	groups := map[string][]int{}
	for i, thumb := range thumbs {
		if thumb.IsLarge {
			continue
		}
		groups[thumb.ThumbURL] = append(groups[thumb.ThumbURL], i)
	}
	for _, indices := range groups {
		if len(indices) == 1 {
			thumb := &thumbs[indices[0]]
			if thumb.BackgroundX == nil {
				v := false
				thumb.IsSprite = &v
			}
			continue
		}
		known, distinct, dimensions := true, true, true
		positions := map[string]bool{}
		for _, i := range indices {
			thumb := thumbs[i]
			if thumb.BackgroundX == nil || thumb.BackgroundY == nil {
				known = false
				continue
			}
			pos := strconv.FormatFloat(*thumb.BackgroundX, 'f', -1, 64) + ":" + strconv.FormatFloat(*thumb.BackgroundY, 'f', -1, 64)
			if positions[pos] {
				distinct = false
			}
			positions[pos] = true
			if thumb.Width <= 0 || thumb.Height <= 0 {
				dimensions = false
			}
		}
		if !known || !distinct || !dimensions {
			continue
		}
		v := true
		for _, i := range indices {
			thumbs[i].IsSprite = &v
		}
	}
}

func findID(n *xhtml.Node, id string) *xhtml.Node {
	return first(n, func(x *xhtml.Node) bool { return x.Type == xhtml.ElementNode && attr(x, "id") == id })
}
func styleDimension(style, key string) float64 {
	m := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(key) + `:\s*(\d+(?:\.\d+)?)px`).FindStringSubmatch(style)
	if len(m) < 2 {
		return 0
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	return v
}
func mapLanguage(key string) string {
	key = strings.TrimSpace(strings.ToLower(key))
	if v := languages[key]; v != "" {
		return v
	}
	if strings.Contains(key, "simplified") {
		return "zh-Hans"
	}
	if strings.Contains(key, "traditional") {
		return "zh-Hant"
	}
	return ""
}

func ParseImagePage(src string) (ImagePageInfo, error) {
	doc, err := parseDoc(src)
	if err != nil {
		return ImagePageInfo{}, err
	}
	img := findID(doc, "img")
	if img == nil {
		return ImagePageInfo{}, fmt.Errorf("image page has no #img element")
	}
	out := ImagePageInfo{ImageURL: attr(img, "src"), Is509: img509[attr(img, "src")]}
	if out.Is509 {
		return out, nil
	}
	w, h := styleDimension(attr(img, "style"), "width"), styleDimension(attr(img, "style"), "height")
	if w > 0 {
		out.Width = &w
	}
	if h > 0 {
		out.Height = &h
	}
	if lf := findID(doc, "loadfail"); lf != nil {
		if m := regexp.MustCompile(`return\s+nl\('([^']*)'\)`).FindStringSubmatch(attr(lf, "onclick")); len(m) > 1 {
			out.ReloadKey = m[1]
		}
	}
	return out, nil
}

func ParseGData(body []byte) ([]GalleryMetadata, error) {
	var raw struct {
		GMetadata []struct {
			GID          json.RawMessage `json:"gid"`
			Token        string          `json:"token"`
			Title        string          `json:"title"`
			TitleJPN     string          `json:"title_jpn"`
			Category     string          `json:"category"`
			Thumb        string          `json:"thumb"`
			Rating       json.RawMessage `json:"rating"`
			Tags         []any           `json:"tags"`
			Filecount    json.RawMessage `json:"filecount"`
			Filesize     json.RawMessage `json:"filesize"`
			Posted       json.RawMessage `json:"posted"`
			Uploader     string          `json:"uploader"`
			Torrentcount json.RawMessage `json:"torrentcount"`
			Expunged     bool            `json:"expunged"`
			Error        string          `json:"error"`
			ParentGID    json.RawMessage `json:"parent_gid"`
			ParentKey    string          `json:"parent_key"`
			FirstGID     json.RawMessage `json:"first_gid"`
			FirstKey     string          `json:"first_key"`
			CurrentGID   json.RawMessage `json:"current_gid"`
			CurrentKey   string          `json:"current_key"`
		} `json:"gmetadata"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	out := make([]GalleryMetadata, 0, len(raw.GMetadata))
	for i, v := range raw.GMetadata {
		// The API can reject individual entries while still returning metadata for
		// the rest of a batch. Rejected entries must never become version records.
		if v.Error != "" {
			continue
		}
		gid, err := gdataInt(v.GID, strconv.IntSize)
		if err != nil {
			return nil, fmt.Errorf("gmetadata[%d].gid: %w", i, err)
		}
		rating, err := gdataFloat(v.Rating)
		if err != nil {
			return nil, fmt.Errorf("gmetadata[%d].rating: %w", i, err)
		}
		filecount, err := gdataInt(v.Filecount, strconv.IntSize)
		if err != nil {
			return nil, fmt.Errorf("gmetadata[%d].filecount: %w", i, err)
		}
		filesize, err := gdataInt(v.Filesize, 64)
		if err != nil {
			return nil, fmt.Errorf("gmetadata[%d].filesize: %w", i, err)
		}
		posted, err := gdataInt(v.Posted, 64)
		if err != nil {
			return nil, fmt.Errorf("gmetadata[%d].posted: %w", i, err)
		}
		torrentcount, err := gdataInt(v.Torrentcount, strconv.IntSize)
		if err != nil {
			return nil, fmt.Errorf("gmetadata[%d].torrentcount: %w", i, err)
		}
		parentGID, err := gdataInt(v.ParentGID, strconv.IntSize)
		if err != nil {
			return nil, fmt.Errorf("gmetadata[%d].parent_gid: %w", i, err)
		}
		firstGID, err := gdataInt(v.FirstGID, strconv.IntSize)
		if err != nil {
			return nil, fmt.Errorf("gmetadata[%d].first_gid: %w", i, err)
		}
		currentGID, err := gdataInt(v.CurrentGID, strconv.IntSize)
		if err != nil {
			return nil, fmt.Errorf("gmetadata[%d].current_gid: %w", i, err)
		}
		m := GalleryMetadata{GID: int(gid), Token: v.Token, Title: v.Title, TitleJPN: v.TitleJPN, Category: v.Category, Thumb: v.Thumb, Rating: rating, Tags: map[string][]GalleryTag{}, Filecount: int(filecount), Filesize: filesize, Posted: posted, Uploader: v.Uploader, Torrentcount: int(torrentcount), Expunged: v.Expunged, ParentGID: int(parentGID), ParentKey: v.ParentKey, FirstGID: int(firstGID), FirstKey: v.FirstKey, CurrentGID: int(currentGID), CurrentKey: v.CurrentKey}
		for _, tag := range v.Tags {
			var ns, key string
			switch item := tag.(type) {
			case string:
				if split := strings.IndexByte(item, ':'); split >= 0 {
					ns, key = item[:split], item[split+1:]
					if ns == "" {
						ns = "temp"
					}
				} else {
					ns, key = "temp", item
				}
			case []any:
				if len(item) < 2 {
					continue
				}
				ns, _ = item[0].(string)
				key, _ = item[1].(string)
			default:
				continue
			}
			if ns != "" && key != "" {
				m.Tags[ns] = append(m.Tags[ns], GalleryTag{Namespace: ns, Key: key, Status: "confidence"})
			}
		}
		out = append(out, m)
	}
	return out, nil
}

func gdataNumber(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "0", nil
	}
	var s string
	if raw[0] == '"' {
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", err
		}
	} else {
		s = string(raw)
	}
	return s, nil
}
func gdataInt(raw json.RawMessage, bits int) (int64, error) {
	s, err := gdataNumber(raw)
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseInt(s, 10, bits)
	if err != nil {
		return 0, fmt.Errorf("invalid integer %q: %w", s, err)
	}
	return v, nil
}
func gdataFloat(raw json.RawMessage) (float64, error) {
	s, err := gdataNumber(raw)
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, fmt.Errorf("invalid float %q", s)
	}
	return v, nil
}

func ParseFavoritesCategories(src string) map[int]string {
	doc, _ := parseDoc(src)
	out := map[int]string{}
	for _, container := range all(doc, func(n *xhtml.Node) bool {
		return n.Type == xhtml.ElementNode && n.Data == "div" && classHas(n, "nosel")
	}) {
		for _, fp := range all(container, func(n *xhtml.Node) bool {
			return n.Type == xhtml.ElementNode && n.Data == "div" && classHas(n, "fp") && attr(n, "onclick") != ""
		}) {
			m := regexp.MustCompile(`favcat=(\d+)`).FindStringSubmatch(attr(fp, "onclick"))
			if len(m) < 2 {
				continue
			}
			id, e := strconv.Atoi(m[1])
			if e != nil {
				continue
			}
			ds := childrenNamed(fp, "div")
			name := ""
			if len(ds) >= 3 {
				name = nodeText(ds[2])
			} else if len(ds) >= 2 {
				name = nodeText(ds[1])
			}
			if name == "" {
				name = nodeText(fp)
			}
			if name != "" {
				out[id] = name
			}
		}
	}
	return out
}

// ParseMyTags extracts the account's highlighted-tag styles. The title
// attribute is the canonical tag name on current EH pages, with f/m/x
// namespace shorthands and occasional namespace-less keys.
func ParseMyTags(src string) map[string]map[string]string {
	doc, _ := parseDoc(src)
	out := map[string]map[string]string{}
	aliases := map[string]string{"f": "female", "m": "male", "x": "mixed"}
	for _, n := range all(doc, func(n *xhtml.Node) bool {
		return n.Type == xhtml.ElementNode && n.Data == "div" && strings.HasPrefix(attr(n, "id"), "tagpreview_") && attr(n, "title") != ""
	}) {
		key := strings.ToLower(strings.Join(strings.Fields(attr(n, "title")), " "))
		if key == "" {
			continue
		}
		if ns, rest, ok := strings.Cut(key, ":"); ok {
			if full, found := aliases[ns]; found {
				ns = full
			}
			key = ns + ":" + rest
		} else {
			key = "*:" + key
		}
		style := parseStyle(attr(n, "style"))
		if len(style) > 0 {
			out[key] = style
		}
	}
	return out
}

func ParseFavcatFromTitle(title string, cats map[int]string) *int {
	for id, name := range cats {
		if name == title {
			x := id
			return &x
		}
	}
	return nil
}

func FilterTagStatus(tags []GalleryTag, level string) []GalleryTag {
	out := []GalleryTag{}
	for _, t := range tags {
		switch level {
		case "strict":
			if t.Status == "confidence" {
				out = append(out, t)
			}
		case "off":
			out = append(out, t)
		default:
			if t.Status == "confidence" || t.Status == "skepticism" {
				out = append(out, t)
			}
		}
	}
	return out
}

func ParsePublishTime(raw string) string {
	for _, layout := range []string{"2006-01-02 15:04", "2 January 2006, 15:04", "2 Jan 2006, 15:04"} {
		if t, e := time.ParseInLocation(layout, strings.TrimSpace(raw), time.UTC); e == nil {
			return t.Format("2006-01-02T15:04:05Z")
		}
	}
	return ""
}
func ParseSizeText(raw string) int64 {
	m := regexp.MustCompile(`(?i)^\s*([\d.]+)\s*([KMGTP]?)\s*(?:i)?B`).FindStringSubmatch(raw)
	if len(m) < 3 {
		return 0
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	power := strings.Index("KMGTP", strings.ToUpper(m[2])) + 1
	return int64(v * math.Pow(1024, float64(power)))
}
func SortedTags(tags []GalleryTag) []GalleryTag {
	out := append([]GalleryTag(nil), tags...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Namespace == out[j].Namespace {
			return out[i].Key < out[j].Key
		}
		return out[i].Namespace < out[j].Namespace
	})
	return out
}
func ResolveURL(href, base string) string {
	u, e := url.Parse(href)
	if e != nil {
		return href
	}
	b, e := url.Parse(base)
	if e != nil {
		return href
	}
	return b.ResolveReference(u).String()
}
func escapeText(s string) string { return html.EscapeString(s) }
