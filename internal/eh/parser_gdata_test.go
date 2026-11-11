package eh

import "testing"

func TestParseGDataNumericValuesAcceptNumbersAndStrings(t *testing.T) {
	got, err := ParseGData([]byte(`{"gmetadata":[{"gid":"12","rating":"4.25","filecount":"8","filesize":"9223372036854770000","posted":"1700000000","torrentcount":"3"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	m := got[0]
	if m.GID != 12 || m.Rating != 4.25 || m.Filecount != 8 || m.Filesize != 9223372036854770000 || m.Posted != 1700000000 || m.Torrentcount != 3 {
		t.Fatalf("unexpected metadata: %+v", m)
	}
	if _, err := ParseGData([]byte(`{"gmetadata":[{"gid":12,"rating":4.25,"filecount":8,"filesize":9,"posted":10,"torrentcount":3}]}`)); err != nil {
		t.Fatal(err)
	}
}

func TestParseGDataVersionFieldsAndPerItemErrors(t *testing.T) {
	got, err := ParseGData([]byte(`{"gmetadata":[{"gid":"12","token":"abc","parent_gid":"9","parent_key":"pk","first_gid":8,"first_key":"fk","current_gid":"12","current_key":"ck"},{"gid":"13","token":"bad","error":"gallery does not exist"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ParentGID != 9 || got[0].ParentKey != "pk" || got[0].FirstGID != 8 || got[0].FirstKey != "fk" || got[0].CurrentGID != 12 || got[0].CurrentKey != "ck" {
		t.Fatalf("unexpected parsed metadata: %+v", got)
	}
}

func TestParseGDataTagsAcceptStringAndPairEntries(t *testing.T) {
	got, err := ParseGData([]byte(`{"gmetadata":[{"tags":["female:twintails","unscoped",":empty-namespace","language:english:variant",["artist","alice"],["female","ponytail"],17,["malformed"]]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	tags := got[0].Tags
	want := map[string][]string{
		"female":   {"twintails", "ponytail"},
		"temp":     {"unscoped", "empty-namespace"},
		"language": {"english:variant"},
		"artist":   {"alice"},
	}
	if len(tags) != len(want) {
		t.Fatalf("tag namespaces = %#v, want %#v", tags, want)
	}
	for namespace, keys := range want {
		actual := tags[namespace]
		if len(actual) != len(keys) {
			t.Fatalf("%s tags = %#v, want keys %#v", namespace, actual, keys)
		}
		for i, key := range keys {
			if actual[i].Key != key || actual[i].Namespace != namespace || actual[i].Status != "confidence" {
				t.Errorf("%s tag[%d] = %+v, want key %q with confidence status", namespace, i, actual[i], key)
			}
		}
	}
}

func TestParseGDataNumericInvalidAndOverflow(t *testing.T) {
	for _, body := range []string{
		`{"gmetadata":[{"gid":"wat"}]}`,
		`{"gmetadata":[{"rating":"nope"}]}`,
		`{"gmetadata":[{"filecount":"1.2"}]}`,
		`{"gmetadata":[{"filesize":"9223372036854775808"}]}`,
		`{"gmetadata":[{"posted":-9223372036854775809}]}`,
		`{"gmetadata":[{"torrentcount":"9223372036854775808"}]}`,
	} {
		if _, err := ParseGData([]byte(body)); err == nil {
			t.Errorf("expected error for %s", body)
		}
	}
	got, err := ParseGData([]byte(`{"gmetadata":[{"gid":null,"rating":null,"filecount":null,"filesize":null,"posted":null,"torrentcount":null}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].GID != 0 || got[0].Rating != 0 || got[0].Filecount != 0 || got[0].Filesize != 0 || got[0].Posted != 0 || got[0].Torrentcount != 0 {
		t.Fatalf("null values not zero: %+v", got[0])
	}
	got, err = ParseGData([]byte(`{"gmetadata":[{}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].GID != 0 || got[0].Rating != 0 || got[0].Filecount != 0 || got[0].Filesize != 0 || got[0].Posted != 0 || got[0].Torrentcount != 0 {
		t.Fatalf("missing values not zero: %+v", got[0])
	}
}
