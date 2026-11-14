package ghtoken

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// fakeAPI is GitHub's API answering 401 to every token but good, and
// answer (200 when nil) otherwise; it keeps the Authorization of each
// request.
type fakeAPI struct {
	*httptest.Server
	mu     sync.Mutex
	auth   []string
	good   string
	answer func(w http.ResponseWriter)
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := r.Header.Get("Authorization")
		f.mu.Lock()
		f.auth = append(f.auth, a)
		good, answer := f.good, f.answer
		f.mu.Unlock()
		if a != "" && a != "Bearer "+good {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"message":"Bad credentials"}`)
			return
		}
		if answer != nil {
			answer(w)
			return
		}
		fmt.Fprint(w, `{"ok":true}`)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeAPI) asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.auth...)
}

func (f *fakeAPI) get(t *testing.T) (*http.Response, bool, []byte) {
	t.Helper()
	req, _ := http.NewRequest("GET", f.URL+"/repos/o/r/commits", nil)
	resp, token, err := Do(req, f.URL, http.DefaultClient.Do)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, token, b
}

func setToken(t *testing.T, settingsTok, githubTok, ghTok string) {
	t.Helper()
	s := settings.Load()
	s.GitHubToken = settingsTok
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_TOKEN", githubTok)
	t.Setenv("GH_TOKEN", ghTok)
}

// till on Discord: a stale GITHUB_TOKEN in the environment made the
// Plugins page say "GitHub refused GITHUB_TOKEN" while GitHub answered
// fine without it. The request is asked again without the token, and the
// anonymous answer is the one given back.
func TestEnvTokenRefusedAskedAgainWithout(t *testing.T) {
	f := newFakeAPI(t)
	setToken(t, "", "stale-env-retry", "")
	resp, token, body := f.get(t)
	if resp.StatusCode != 200 || token || string(body) != `{"ok":true}` {
		t.Fatalf("got %d token=%v %s", resp.StatusCode, token, body)
	}
	if a := f.asked(); len(a) != 2 || a[0] != "Bearer stale-env-retry" || a[1] != "" {
		t.Errorf("GitHub was asked with %q, want the token then none", a)
	}
}

// Once refused, the environment's token isn't sent again: the next
// request goes without it at once, and Token says there is none.
func TestEnvTokenRefusedNotSentAgain(t *testing.T) {
	f := newFakeAPI(t)
	setToken(t, "", "stale-env-once", "")
	f.get(t)
	n := len(f.asked())
	resp, token, _ := f.get(t)
	if resp.StatusCode != 200 || token {
		t.Fatalf("second request: %d token=%v", resp.StatusCode, token)
	}
	if a := f.asked()[n:]; len(a) != 1 || a[0] != "" {
		t.Errorf("after the refusal GitHub was asked with %q, want one request without a token", a)
	}
	if tok, from := Token(); tok != "" || from != "" {
		t.Errorf("Token() = %q from %q after its refusal", tok, from)
	}
	// GH_TOKEN, a different token, is still tried
	t.Setenv("GH_TOKEN", "good-gh")
	f.mu.Lock()
	f.good = "good-gh"
	f.mu.Unlock()
	if _, token, _ := f.get(t); !token {
		t.Error("GH_TOKEN wasn't sent after GITHUB_TOKEN was refused")
	}
}

// The token set in Settings is the user's own choice, so its refusal is
// said — and the check goes on: the request is asked again without it, the
// anonymous answer is the one given back, and the token isn't sent again
// while that field holds it (#3926).
func TestSettingsTokenRefusedAskedAgainWithout(t *testing.T) {
	f := newFakeAPI(t)
	setToken(t, "ghp_settings_stale", "", "")
	resp, token, body := f.get(t)
	if resp.StatusCode != 200 || token || string(body) != `{"ok":true}` {
		t.Fatalf("got %d token=%v %s", resp.StatusCode, token, body)
	}
	if a := f.asked(); len(a) != 2 || a[0] != "Bearer ghp_settings_stale" || a[1] != "" {
		t.Errorf("GitHub was asked with %q, want the token then none", a)
	}
	if _, ok := SettingsRefused(); !ok {
		t.Error("SettingsRefused() says the Settings token was not refused")
	}
	err := Refused()
	if !strings.Contains(err.Error(), "the GitHub token in Settings") || strings.Contains(err.Error(), "ghp_settings_stale") {
		t.Errorf("Refused() = %v", err)
	}
	// it isn't sent again: the next request goes without it at once
	n := len(f.asked())
	if _, token, _ := f.get(t); token {
		t.Error("the refused Settings token was sent again")
	}
	if a := f.asked()[n:]; len(a) != 1 || a[0] != "" {
		t.Errorf("after the refusal GitHub was asked with %q, want one request without a token", a)
	}
	if tok, from := Token(); tok != "" || from != "" {
		t.Errorf("Token() = %q from %q after its refusal", tok, from)
	}
}

// A token typed into Settings after one was refused is another token: it is
// sent, and the page stops saying the old one was refused.
func TestSettingsTokenReplacedAfterRefusalIsSent(t *testing.T) {
	f := newFakeAPI(t)
	setToken(t, "ghp_settings_old", "", "")
	f.get(t)
	f.mu.Lock()
	f.good = "ghp_settings_new"
	f.mu.Unlock()
	setToken(t, "ghp_settings_new", "", "")
	if _, token, _ := f.get(t); !token {
		t.Error("the token typed after the refusal wasn't sent")
	}
	if _, ok := SettingsRefused(); ok {
		t.Error("SettingsRefused() still says a token was refused after another was typed")
	}
}

// The anonymous request, rate limited, is said as GitHub's limit for
// requests without a token, as before.
func TestEnvTokenRefusedThenAnonymousLimited(t *testing.T) {
	f := newFakeAPI(t)
	f.answer = func(w http.ResponseWriter) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1893553440")
		w.WriteHeader(403)
		fmt.Fprint(w, `{"message":"API rate limit exceeded for 1.2.3.4."}`)
	}
	setToken(t, "", "", "stale-env-limited")
	resp, token, body := f.get(t)
	e, ok := LimitedBy(resp, body, token)
	if !ok || e.Token || e.Until.IsZero() {
		t.Fatalf("LimitedBy = %+v %v (status %d)", e, ok, resp.StatusCode)
	}
	var l Limited
	if err := error(e); !errors.As(err, &l) || !strings.Contains(err.Error(), "60 an hour") {
		t.Errorf("limit said as %q", err)
	}
}

// A request to another host never carries the token, and its 401 is left
// as it is.
func TestOtherHostUntouched(t *testing.T) {
	f := newFakeAPI(t)
	setToken(t, "", "env-other-host", "")
	req, _ := http.NewRequest("GET", f.URL+"/x", nil)
	resp, token, err := Do(req, "https://api.github.invalid", http.DefaultClient.Do)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if token || resp.StatusCode != 200 {
		t.Errorf("got %d token=%v", resp.StatusCode, token)
	}
	if a := f.asked(); len(a) != 1 || a[0] != "" {
		t.Errorf("another host was asked with %q", a)
	}
}

// A refused Settings token isn't swapped for the environment's: the user
// chose the one in Settings over it, and the page says requests go without
// a token, so they do (#3926).
func TestSettingsTokenRefusedGoesWithoutEnvToken(t *testing.T) {
	f := newFakeAPI(t)
	f.mu.Lock()
	f.good = "ghp_env_good"
	f.mu.Unlock()
	setToken(t, "ghp_settings_gone", "ghp_env_good", "")
	f.get(t)
	n := len(f.asked())
	if _, token, _ := f.get(t); token {
		t.Error("a token was sent after the Settings one was refused")
	}
	if a := f.asked()[n:]; len(a) != 1 || a[0] != "" {
		t.Errorf("after the Settings token's refusal GitHub was asked with %q, want none", a)
	}
	if tok, from := Token(); tok != "" || from != "" {
		t.Errorf("Token() = %q from %q, want none", tok, from)
	}
}

// A mirror answers a request sent it without the token (source.Do,
// #3923): that answer didn't carry the token, and a 401 from it isn't
// GitHub refusing the token, which goes on being sent.
func TestMirrorAnswerIsNotTheToken(t *testing.T) {
	setToken(t, "ghp_settings_good", "", "")
	for _, status := range []int{200, 401} {
		var asked []string
		send := func(r *http.Request) (*http.Response, error) {
			asked = append(asked, r.Header.Get("Authorization"))
			bare := r.Clone(r.Context())
			bare.Header.Del("Authorization")
			return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{}")), Request: bare}, nil
		}
		req, _ := http.NewRequest("GET", "https://api.github.com/repos/o/r", nil)
		resp, token, err := Do(req, "https://api.github.com", send)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if token || resp.StatusCode != status || len(asked) != 1 {
			t.Fatalf("%d from a mirror: token=%v, status %d, asked %q", status, token, resp.StatusCode, asked)
		}
		if _, ok := SettingsRefused(); ok {
			t.Fatalf("%d from a mirror: the Settings token counted refused", status)
		}
		if tok, _ := Token(); tok != "ghp_settings_good" {
			t.Fatalf("%d from a mirror: Token() = %q", status, tok)
		}
	}
}
