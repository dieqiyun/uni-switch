package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agentenv"
)

// isolateFetchHome keeps a fetch's fixtures out of the user's own uni-switch
// home: the list a fetch reads and writes is under HOME, and the tests
// below save providers of their own.
func isolateFetchHome(t *testing.T) {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	t.Setenv("PATH", h)
	for _, v := range agentenv.Vars {
		t.Setenv(v, "")
	}
}

// One base string is not one request (#1006). A vendor is configured with
// the same URL for Chat, Anthropic and Gemini, and answers each of them
// only under the authentication that protocol uses: the Chat request is
// answered 401, and the Anthropic and Gemini lists are there for the
// asking. Deduping by the configured base string took the latter two for
// lists already had, because a request had been made at that base, and
// the user was left with "no model to try" for a vendor whose models were
// there the whole time.
func TestFetchAsksEachAuthenticationAtOneBase(t *testing.T) {
	isolateFetchHome(t)
	var mu sync.Mutex
	asked := map[string]int{}
	note := func(k string) {
		mu.Lock()
		asked[k]++
		mu.Unlock()
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Header.Get("x-goog-api-key") != "":
			note("gemini")
			io.WriteString(w, `{"models":[{"name":"models/gemini-native","supportedGenerationMethods":["generateContent"]}]}`)
		case r.Header.Get("anthropic-version") != "":
			note("anthropic")
			io.WriteString(w, `{"data":[{"id":"claude-native","display_name":"Claude Native"}]}`)
		default:
			note("chat")
			http.Error(w, `{"error":{"message":"the key is not for this API"}}`, http.StatusUnauthorized)
		}
	}))
	t.Cleanup(srv.Close)

	base := srv.URL + "/v1beta"
	if err := Save(Provider{ID: "triple", Name: "Triple", Chat: base, Anthropic: base, Gemini: base, Key: "sk-t"}); err != nil {
		t.Fatal(err)
	}
	q, err := Find("triple")
	if err != nil {
		t.Fatal(err)
	}
	ms, err := q.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	got := map[string]bool{}
	for _, m := range ms {
		got[m.ID] = true
	}
	for _, id := range []string{"claude-native", "gemini-native"} {
		if !got[id] {
			t.Errorf("%s not listed: %v", id, ms)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if asked["chat"] != 1 {
		t.Errorf("chat asked %d times, want once", asked["chat"])
	}
	// each list at this base asked under its own authentication, and
	// neither taken for the refusal the one before it met
	if asked["anthropic"] != 1 {
		t.Errorf("anthropic asked %d times, want once", asked["anthropic"])
	}
	if asked["gemini"] != 1 {
		t.Errorf("gemini asked %d times, want once", asked["gemini"])
	}
}

// Two bases are not two lists (#1006). A remote uni-switch is configured with
// its Chat base at …/v1 and its Anthropic base at the bare host beside
// it, and both resolve to one list URL, GET /v1/models: the test of a
// saved remote is to fetch that list once, because its decision models
// are marked out of it. Deduping by the configured base string asked the
// one list twice, once per string.
func TestFetchAsksOneListOnceForTwoBasesOfIt(t *testing.T) {
	isolateFetchHome(t)
	var mu sync.Mutex
	asked := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		k := r.URL.Path
		if r.URL.Query().Has("client_version") {
			// Codex's own ask of the Responses list (codexEntries),
			// a request of its own
			k += "?client_version"
		}
		mu.Lock()
		asked[k]++
		mu.Unlock()
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, `{"data":[{"id":"lib/m1","display_name":"M1"}]}`)
	}))
	t.Cleanup(srv.Close)

	if err := Save(Provider{ID: "paired", Name: "Paired", Chat: srv.URL + "/v1", Anthropic: srv.URL, Key: "sk-p"}); err != nil {
		t.Fatal(err)
	}
	q, err := Find("paired")
	if err != nil {
		t.Fatal(err)
	}
	ms, err := q.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(ms) != 1 || ms[0].ID != "lib/m1" {
		t.Fatalf("listed %v, want lib/m1", ms)
	}
	mu.Lock()
	if asked["/v1/models"] != 1 {
		t.Errorf("the one list fetched %d times, want once", asked["/v1/models"])
	}
	if asked["/models"] != 0 {
		t.Errorf("/models asked %d times although the /v1/models beside it had answered", asked["/models"])
	}
	mu.Unlock()
	// and the same base as Chat and Responses is one list too, the one
	// case the base string already covered; Codex's own ask of the
	// Responses list is counted apart (#3854)
	if err := Save(Provider{ID: "one", Name: "One", Chat: srv.URL + "/v1", Responses: srv.URL + "/v1", Key: "sk-p"}); err != nil {
		t.Fatal(err)
	}
	o, err := Find("one")
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	asked["/v1/models"] = 0
	mu.Unlock()
	if _, err := o.Fetch(context.Background()); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if asked["/v1/models"] != 1 {
		t.Errorf("chat and responses fetched the list %d times, want once", asked["/v1/models"])
	}
	if n := asked["/v1/models?client_version"]; n > 1 {
		t.Errorf("the list asked as Codex asks %d times, want once at most", n)
	}
}

// What a base's list refused is reported once (#1006). Chat and Responses
// at one base are one request, and the second of them has nothing left to
// ask — which is not a second refusal, and saying so would put a sentence
// about the dedup in the middle of the message the user reads about their
// key. The one refusal stands alone.
func TestFetchReportsOneRefusalForOneBaseAskedTwice(t *testing.T) {
	isolateFetchHome(t)
	var mu sync.Mutex
	asked := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":{"message":"the key is not for this API"}}`, http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	if err := Save(Provider{ID: "twice", Name: "Twice", Chat: srv.URL + "/v1", Responses: srv.URL + "/v1", Key: "sk-x"}); err != nil {
		t.Fatal(err)
	}
	q, err := Find("twice")
	if err != nil {
		t.Fatal(err)
	}
	_, err = q.Fetch(context.Background())
	if err == nil {
		t.Fatal("a list refused for every protocol fetched")
	}
	msg := err.Error()
	if n := strings.Count(msg, "401 Unauthorized"); n != 1 {
		t.Errorf("the one refusal named %d times: %s", n, msg)
	}
	for _, noise := range []string{"already asked", "no model to try"} {
		if strings.Contains(msg, noise) {
			t.Errorf("the message says %q about the dedup: %s", noise, msg)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if asked != 1 {
		t.Errorf("the list asked %d times, want once", asked)
	}
}
