package eh

import (
	"testing"
)

func TestExtractAdvancedQueryPreservesQuotedExcludePhrases(t *testing.T) {
	input := `l:chinese expunged -o:"rough translation$" -o:"extraneous ads$"`
	got, params := ExtractAdvancedQuery(input)
	want := `l:chinese -o:"rough translation$" -o:"extraneous ads$"`
	if got != want {
		t.Fatalf("clean query=%q, want %q", got, want)
	}
	if params.Get("f_sh") != "on" || params.Get("advsearch") != "1" {
		t.Fatalf("advanced params=%v, want f_sh=on and advsearch=1", params)
	}
}
