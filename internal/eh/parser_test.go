package eh

import "testing"

const extendedSiblingCellListHTML = `<!doctype html><html><body>
<div id="nb" class="nosel"><div><a href="https://e-hentai.org/">Front Page</a></div></div>
<div class="nosel"><div class="fp" onclick="popUp('/gallerypopups.php?favcat=1')"><div>1</div><div>Favorite</div><div>Common</div></div></div>
<table class="itg glte"><tr>
  <td class="gl1e"><div><a href="https://e-hentai.org/g/42/abcd/"><img data-src="https://ehgt.org/test-cover.webp" src="placeholder.gif"></a></div></td>
  <td class="gl2e"><div>
    <div class="gl3e"><div class="cn">Manga</div><div id="posted_42" title="Common">2026-08-11 12:34</div>
      <div class="ir" style="background-position:-16px -21px"></div>
      <div><a href="https://e-hentai.org/uploader/fixture">Fixture Uploader</a></div>
      <div>42 pages</div><div>0 torrents</div>
    </div>
    <a href="https://e-hentai.org/g/42/abcd/"><div class="gl4e glname">
      <div class="glink"><s>Fixture Manga</s></div><div><table><tr><td>
        <div class="gt" title="language:japanese">japanese</div>
        <div class="gtl" title="female:sample tag" style="color:#FFFFFF;border-color:#FFFFFF;background:#000000 !important">sample tag</div>
      </td></tr></table></div>
    </div></a>
  </div></td>
</tr></table>
<a href="/?next=42-1786365950" id="unext">Next</a>
</body></html>`

func TestParseExtendedListRowsUseSiblingMetadataCell(t *testing.T) {
	info, err := ParseListPage(extendedSiblingCellListHTML)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Galleries) != 1 {
		t.Fatalf("gallery count=%d, want one", len(info.Galleries))
	}
	g := info.Galleries[0]
	if g.GID != 42 || g.Token != "abcd" || g.Title != "Fixture Manga" || g.Category != "Manga" {
		t.Fatalf("identity/title/category=%#v", g)
	}
	if g.CoverURL != "https://ehgt.org/test-cover.webp" || g.PageCount == nil || *g.PageCount != 42 {
		t.Fatalf("cover/page_count=%q/%v", g.CoverURL, g.PageCount)
	}
	if g.Rating != 3.5 || g.PublishTime != "2026-08-11 12:34" || g.Uploader != "Fixture Uploader" || g.Language != "ja" {
		t.Fatalf("extra metadata rating=%v posted=%q uploader=%q language=%q", g.Rating, g.PublishTime, g.Uploader, g.Language)
	}
	if g.Favcat == nil || *g.Favcat != 1 || !g.Expunged {
		t.Fatalf("favcat/expunged=%v/%v", g.Favcat, g.Expunged)
	}
	if len(g.Tags) != 2 || g.Tags[0].String() != "language:japanese" || g.Tags[1].String() != "female:sample tag" {
		t.Fatalf("tags=%#v", g.Tags)
	}
	if g.Tags[1].Status != "skepticism" || g.Tags[1].Style["color"] != "#FFFFFF" || g.Tags[1].Style["borderColor"] != "#FFFFFF" || g.Tags[1].Style["background"] != "#000000" {
		t.Fatalf("tag status/style=%#v", g.Tags[1])
	}
	if info.NextGID != "42-1786365950" {
		t.Fatalf("compound next cursor=%q", info.NextGID)
	}
}

func TestParseListPageKeepsLooseGenericRowFallback(t *testing.T) {
	page := `<html><body><div class="gl1e"><a href="/g/9/abcdef/"><div class="glink">Legacy title</div></a>
		<div class="cn">Doujinshi</div><img src="/legacy-cover.jpg">12 pages</div></body></html>`
	info, err := ParseListPage(page)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Galleries) != 1 || info.Galleries[0].Title != "Legacy title" || info.Galleries[0].Category != "Doujinshi" || info.Galleries[0].CoverURL != "/legacy-cover.jpg" || info.Galleries[0].PageCount == nil || *info.Galleries[0].PageCount != 12 {
		t.Fatalf("generic fallback item=%#v", info.Galleries)
	}
}

func TestParseDetailPageCommentsAndMyTags(t *testing.T) {
	page := `<!doctype html><html><body>
	<div id="gdt"><div class="gdtm"><a href="/s/abc/123-1"><div style="background:transparent url(https://ehgt.org/t/thumb.jpg) no-repeat;width:100px;height:100px"></div></a></div></div>
	<div id="taglist"><div id="td_f:catgirl" class="gt"></div></div>
	<div id="cdiv"><div class="c1"><div class="c2"><div class="c3">Posted on 14 January 2024, 09:35 by: <a href="/u/1">Alice</a> <a href="/forums.php?showuser=42">profile</a></div></div>
	<div class="c6" id="comment_12"><b>Hello</b> <a href="https://e-hentai.org/g/77/abcdef/">gallery</a><img src="https://ehgt.org/w/cover.webp"></div>
	<div class="c8">Edited <strong>15 January 2024, 10:00</strong></div></div></div></body></html>`
	detail, err := ParseDetailPage(page, "e-hentai.org", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Comments) != 1 {
		t.Fatalf("comments=%d, want 1", len(detail.Comments))
	}
	c := detail.Comments[0]
	if c.ID != 12 || c.Username != "Alice" || c.UserID == nil || *c.UserID != 42 || c.Time != "2024-01-14 09:35" {
		t.Fatalf("unexpected parsed comment metadata: %#v", c)
	}
	if c.LastEditTime != "15 January 2024, 10:00" || c.ContentHTML == "" {
		t.Fatalf("comment edit/content missing: %#v", c)
	}
	styles := ParseMyTags(`<div id="tagpreview_1" title="f:catgirl" style="color:#f1f1f1;border-color:#123;background:radial-gradient(red,blue) !important"></div>
	<div id="tagpreview_2" title="Unscoped Tag" style="background:#abc"></div>
	<div id="tagpreview_3" title="male:unmarked"></div>`)
	if got := styles["female:catgirl"]["borderColor"]; got != "#123" {
		t.Fatalf("borderColor=%q", got)
	}
	if got := styles["female:catgirl"]["background"]; got != "radial-gradient(red,blue)" {
		t.Fatalf("background=%q", got)
	}
	if styles["*:unscoped tag"]["background"] != "#abc" {
		t.Fatalf("wildcard tag style missing: %#v", styles)
	}
	if _, ok := styles["male:unmarked"]; ok {
		t.Fatalf("unstyled My Tags item should be omitted: %#v", styles)
	}
}

func TestParseDetailPageNewerVersionsOnlyFromGNDWithDedupeAndTailDates(t *testing.T) {
	page := `<!doctype html><html><body>
	<div id="gdt"><div class="gdtm"><a href="/s/abc/123-1"><div style="background:url(https://ehgt.org/t/thumb.jpg);width:100px;height:100px"></div></a></div></div>
	<a href="https://e-hentai.org/g/700/deadbeef/">Unrelated gallery link</a>
	<div id="gnd">New versions:
		<a href="/g/936128/585a8242de/">First &amp; title</a> Added on 2024-01-02 12:00
		<a href="/g/936128/585a8242de/">Duplicate title</a> Added on 2026-01-01 12:00
		<a href="https://e-hentai.org/g/999/abcdef/">Second &amp; newer</a> Added on 3 February 2025, 09:30
		<a href="/g/1000/abcdef/">Undated final</a>
		<a href="/g/1100/deed/">Recoverable date</a> Added on 2024-99-99 99:99; corrected on 4 March 2026, 10:10
		<a href="/g/bad/token/">bad</a>
	</div></body></html>`
	detail, err := ParseDetailPage(page, "e-hentai.org", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.NewerVersions) != 4 {
		t.Fatalf("newer versions=%#v, want four unique #gnd links", detail.NewerVersions)
	}
	want := []GalleryVersion{
		{GID: 936128, Token: "585a8242de", Title: "First & title", AddedAt: "2024-01-02T12:00:00Z"},
		{GID: 999, Token: "abcdef", Title: "Second & newer", AddedAt: "2025-02-03T09:30:00Z"},
		{GID: 1000, Token: "abcdef", Title: "Undated final"},
		{GID: 1100, Token: "deed", Title: "Recoverable date", AddedAt: "2026-03-04T10:10:00Z"},
	}
	for i := range want {
		if detail.NewerVersions[i] != want[i] {
			t.Fatalf("version[%d]=%#v, want %#v", i, detail.NewerVersions[i], want[i])
		}
	}
}
