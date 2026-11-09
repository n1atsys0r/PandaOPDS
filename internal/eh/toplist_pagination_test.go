package eh

import "testing"

func TestParseListPageLeavesToplistNextUnsetOnLastPage(t *testing.T) {
	page := `<!doctype html><html><body>
	<div class="ptt"><table><tr>
		<td><a href="/toplist.php?tl=15&amp;p=0">1</a></td>
		<td>Next &rsaquo;</td>
	</tr></table></div>
	</body></html>`
	info, err := ParseListPage(page)
	if err != nil {
		t.Fatal(err)
	}
	if info.NextPage != nil {
		t.Fatalf("last page next=%v, want nil", *info.NextPage)
	}
}
