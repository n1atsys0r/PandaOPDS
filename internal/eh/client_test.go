package eh

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"testing"

	"pandaopds/internal/config"
)

type profileCall struct {
	method  string
	path    string
	referer string
	form    url.Values
}

func establishWithProfilePage(t *testing.T, profile, page string) []profileCall {
	t.Helper()
	var calls []profileCall
	cfg := config.Settings{EHSite: "e-hentai", EHProfile: profile, TimeoutSeconds: 2, HTMLIntervalSeconds: 0, Retries: 0}
	client := NewClient(cfg, replayRoundTripper(func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		call := profileCall{method: r.Method, path: r.URL.Path, referer: r.Referer()}
		if r.Method == http.MethodPost {
			call.form, err = url.ParseQuery(string(body))
			if err != nil {
				return nil, err
			}
		}
		calls = append(calls, call)
		responseBody := `<html>session</html>`
		if r.URL.Path == "/uconfig.php" && r.Method == http.MethodGet {
			responseBody = page
		}
		return replayResponse(r, http.StatusOK, responseBody), nil
	}), NewThrottle(cfg))
	if err := client.EstablishSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	return calls
}

func TestEstablishSessionSwitchesToMatchingNumericProfileWithReferer(t *testing.T) {
	calls := establishWithProfilePage(t, "Panda OPDS", `<select><option value="9">Panda OPDS</option></select>
	<form id="profile_form"><select>
		<option value="default">Panda OPDS</option><option value="37">Panda OPDS</option>
	</select></form>`)
	if len(calls) != 3 || calls[0].path != "/" || calls[1].path != "/uconfig.php" || calls[2].method != http.MethodPost {
		t.Fatalf("profile calls=%#v", calls)
	}
	post := calls[2]
	if post.form.Get("profile_set") != "37" || post.form.Get("profile_action") != "" {
		t.Fatalf("profile switch form=%v, want profile_set=37", post.form)
	}
	if post.referer != "https://e-hentai.org/uconfig.php" {
		t.Fatalf("profile switch Referer=%q", post.referer)
	}
}

func TestEstablishSessionCreatesMissingProfileWithReferer(t *testing.T) {
	calls := establishWithProfilePage(t, "Panda OPDS", `<form id="profile_form"><select><option value="1">Other</option></select></form>`)
	if len(calls) != 3 || calls[2].method != http.MethodPost {
		t.Fatalf("profile calls=%#v", calls)
	}
	post := calls[2]
	if post.form.Get("profile_action") != "create" || post.form.Get("profile_name") != "Panda OPDS" || post.form.Get("profile_set") != "616" {
		t.Fatalf("profile create form=%v", post.form)
	}
	if post.referer != "https://e-hentai.org/uconfig.php" {
		t.Fatalf("profile create Referer=%q", post.referer)
	}
}

func TestEmptyEHProfileDisablesUConfigRequests(t *testing.T) {
	calls := establishWithProfilePage(t, "", `<select><option value="1">Panda OPDS</option></select>`)
	if len(calls) != 1 || calls[0].method != http.MethodGet || calls[0].path != "/" {
		t.Fatalf("EH_PROFILE empty still touched uconfig: %#v", calls)
	}
}

func TestListURLUsesExactlyOneExtendedLayoutParameter(t *testing.T) {
	cfg := config.Settings{EHSite: "e-hentai"}
	client := NewClient(cfg, nil, NewThrottle(cfg))
	list, err := url.Parse(client.ListURL("latest", "", url.Values{"inline_set": {"wrong", "duplicate"}}))
	if err != nil {
		t.Fatal(err)
	}
	if got := list.Query()["inline_set"]; len(got) != 1 || got[0] != "dm_e" {
		t.Fatalf("list inline_set=%v, want exactly [dm_e]", got)
	}
	toplist, err := url.Parse(client.ToplistURL("yesterday", 1))
	if err != nil {
		t.Fatal(err)
	}
	if got := toplist.Query()["inline_set"]; len(got) != 1 || got[0] != "dm_e" {
		t.Fatalf("toplist inline_set=%v, want exactly [dm_e]", got)
	}
}
