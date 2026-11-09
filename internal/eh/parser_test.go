package eh

import "testing"

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
