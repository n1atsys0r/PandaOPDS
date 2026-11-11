package eh

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"pandaopds/internal/config"
)

func TestGDataBatchUsesSiteAPIChunksAndFiltersIdentity(t *testing.T) {
	cfg := config.Settings{EHSite: "exhentai", TimeoutSeconds: 2, HTMLIntervalSeconds: 0, Retries: 0}
	var calls int
	client := NewClient(cfg, replayRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			return replayResponse(r, http.StatusOK, `<html>session</html>`), nil
		}
		calls++
		if r.URL.String() != "https://api.e-hentai.org/api.php" {
			t.Errorf("API URL=%s", r.URL)
		}
		var req struct {
			GIDList [][]any `json:"gidlist"`
		}
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &req); err != nil {
			t.Fatal(err)
		}
		if len(req.GIDList) > 25 {
			t.Errorf("sent %d entries", len(req.GIDList))
		}
		return replayResponse(r, http.StatusOK, `{"gmetadata":[{"gid":1,"token":"ok"},{"gid":2,"token":"wrong"},{"gid":3,"token":"bad","error":"denied"}]}`), nil
	}), NewThrottle(cfg))
	got, err := client.GDataBatch(context.Background(), []GalleryRef{{1, "ok"}, {2, "right"}, {3, "bad"}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(got) != 1 || got[0].GID != 1 || got[0].Token != "ok" {
		t.Fatalf("calls=%d results=%+v", calls, got)
	}
}

func TestGDataDispatchScheduleEnforcesBurstCooldown(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for count := 0; count < gdataBurstLimit; count++ {
		wait, next := gdataDispatchSchedule(base.Add(time.Duration(count)*gdataMinSpacing), func() time.Time {
			if count == 0 {
				return time.Time{}
			}
			return base.Add(time.Duration(count-1) * gdataMinSpacing)
		}(), count)
		if wait != 0 || next != count+1 {
			t.Fatalf("dispatch %d: wait=%s count=%d", count+1, wait, next)
		}
	}
	wait, next := gdataDispatchSchedule(base.Add(4*gdataMinSpacing), base.Add(3*gdataMinSpacing), gdataBurstLimit)
	if wait != gdataBurstWindow-gdataMinSpacing || next != 1 {
		t.Fatalf("burst cooldown: wait=%s count=%d", wait, next)
	}
	wait, next = gdataDispatchSchedule(base.Add(gdataBurstWindow), base, gdataBurstLimit)
	if wait != 0 || next != 1 {
		t.Fatalf("idle reset: wait=%s count=%d", wait, next)
	}
}

func TestRequestDoesNotRetryAfterCircuitOpens(t *testing.T) {
	cfg := config.Settings{TimeoutSeconds: 2, Retries: 1}
	throttle := NewThrottle(cfg)
	var attempts int
	client := NewClient(cfg, replayRoundTripper(func(r *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			throttle.TripFromBody("Your IP address is temporarily banned", KindHTML, "e-hentai.org")
		}
		return replayResponse(r, http.StatusServiceUnavailable, "temporary error"), nil
	}), throttle)
	_, _, err := client.Get(context.Background(), "https://ehgt.org/preview.webp", KindThumb)
	var circuitErr *CircuitOpenError
	if attempts != 1 || !errors.As(err, &circuitErr) || circuitErr.Reason != "banned" {
		t.Fatalf("attempts=%d error=%v; want one attempt and an open banned circuit", attempts, err)
	}
}

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
