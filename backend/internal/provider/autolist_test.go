package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/agentenv"
	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

func autolistHome(t *testing.T) {
	t.Helper()
	isolate(t)
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

// relayList serves an OpenAI-compatible /v1/models of the ids it holds,
// and counts the times it was asked.
type relayList struct {
	ids   atomic.Value
	asked atomic.Int32
	srv   *httptest.Server
}

func newRelayList(t *testing.T, ids ...string) *relayList {
	r := &relayList{}
	r.ids.Store(ids)
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/models" {
			http.NotFound(w, req)
			return
		}
		r.asked.Add(1)
		var data []map[string]any
		for _, id := range r.ids.Load().([]string) {
			data = append(data, map[string]any{"id": id, "object": "model", "owned_by": "relay"})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *relayList) set(ids ...string) { r.ids.Store(ids) }

func servedIDs(t *testing.T, id string) []string {
	t.Helper()
	p, err := Find(id)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range p.Exposed() {
		out = append(out, m.ID)
	}
	return out
}

func refetchRelay(t *testing.T, id string) []string {
	t.Helper()
	p, err := Find(id)
	if err != nil {
		t.Fatal(err)
	}
	_, _, taken, err := p.RefetchTaking(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return taken
}

// A relay whose user picked a few models and set rules (#2135): the rules
// take the matching models it lists now when they are set, then each
// matching model a later fetch newly lists. A newly listed model no rule
// matches stays out, the picks the user made stay, and a matching model the
// user unticked afterwards stays unticked on the next fetch.
func TestAutoIncludeTakesNewlyListedMatches(t *testing.T) {
	autolistHome(t)
	r := newRelayList(t, "gpt-6", "gpt-6-mini", "claude-sonnet-5", "deepseek-v4", "glm-5")
	if err := Save(Provider{ID: "relay", Name: "Relay", Chat: r.srv.URL + "/v1", Key: "sk-relay"}); err != nil {
		t.Fatal(err)
	}
	refetchRelay(t, "relay")
	p, _ := Find("relay")
	p.Models = []string{"deepseek-v4"}
	if err := Save(*p); err != nil {
		t.Fatal(err)
	}

	taken, err := SetAutoInclude("relay", []string{" gpt-* ", "re:claude-.*", "gpt-*"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"gpt-6", "gpt-6-mini", "claude-sonnet-5"}; !slices.Equal(taken, want) {
		t.Fatalf("taken when the rules were set: %v, want %v", taken, want)
	}
	if got, want := servedIDs(t, "relay"), []string{"gpt-6", "gpt-6-mini", "claude-sonnet-5", "deepseek-v4"}; !slices.Equal(got, want) {
		t.Fatalf("exposed after the rules were set: %v, want %v", got, want)
	}
	if p, _ := Find("relay"); !slices.Equal(p.AutoInclude, []string{"gpt-*", "re:claude-.*"}) {
		t.Fatalf("rules kept as %q", p.AutoInclude)
	}

	// the user unticks gpt-6-mini; the vendor adds gpt-7, claude-opus-5
	// and qwen-4
	p, _ = Find("relay")
	p.Models = []string{"gpt-6", "claude-sonnet-5", "deepseek-v4"}
	if err := Save(*p); err != nil {
		t.Fatal(err)
	}
	r.set("gpt-7", "gpt-6", "gpt-6-mini", "claude-opus-5", "claude-sonnet-5", "deepseek-v4", "glm-5", "qwen-4")
	if got, want := refetchRelay(t, "relay"), []string{"gpt-7", "claude-opus-5"}; !slices.Equal(got, want) {
		t.Fatalf("taken by the fetch: %v, want %v", got, want)
	}
	if got, want := servedIDs(t, "relay"), []string{"gpt-7", "claude-opus-5", "gpt-6", "claude-sonnet-5", "deepseek-v4"}; !slices.Equal(got, want) {
		t.Fatalf("exposed after the fetch: %v, want %v", got, want)
	}

	// nothing new: nothing taken, and providers.json isn't written
	before, _ := os.ReadFile(Path())
	if got := refetchRelay(t, "relay"); len(got) != 0 {
		t.Fatalf("a fetch with nothing new took %v", got)
	}
	if after, _ := os.ReadFile(Path()); string(after) != string(before) {
		t.Fatalf("providers.json written by a fetch with nothing to take:\n%s\n%s", before, after)
	}
}

// With nothing picked, a provider serves the first of its vendor's list
// (manyModels). A rule adds the matching models past that slice, and keeps
// every model served before: it never takes one away.
func TestAutoIncludeKeepsWhatIsServedWithNothingPicked(t *testing.T) {
	autolistHome(t)
	var ids []string
	for i := range 30 {
		ids = append(ids, "m-"+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	ids = append(ids, "special-1")
	r := newRelayList(t, ids...)
	if err := Save(Provider{ID: "relay", Name: "Relay", Chat: r.srv.URL + "/v1", Key: "sk-relay"}); err != nil {
		t.Fatal(err)
	}
	refetchRelay(t, "relay")
	served := servedIDs(t, "relay")
	if len(served) != manyModels || slices.Contains(served, "special-1") {
		t.Fatalf("served before the rule: %v", served)
	}
	taken, err := SetAutoInclude("relay", []string{"special-*"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(taken, []string{"special-1"}) {
		t.Fatalf("taken: %v", taken)
	}
	got := servedIDs(t, "relay")
	if want := append([]string{"special-1"}, served...); !slices.Equal(got, want) {
		t.Fatalf("served after the rule: %v, want %v", got, want)
	}
}

// A rule must be a pattern, and readable; an account's provider takes none.
func TestAutoIncludeRefusesWhatIsNoRule(t *testing.T) {
	for _, bad := range [][]string{{"gpt-6"}, {"re:("}, {"re:"}} {
		if _, err := CleanAutoInclude(bad); err == nil {
			t.Errorf("CleanAutoInclude(%q) took it", bad)
		}
	}
	if got, err := CleanAutoInclude([]string{"", "*", "re:a|b"}); err != nil || !slices.Equal(got, []string{"*", "re:a|b"}) {
		t.Errorf("CleanAutoInclude: %v %v", got, err)
	}
	if (Provider{ID: "codex", Account: &Account{}}).ownList() {
		t.Error("an account's provider is its own list's to keep")
	}
}

func TestParseRefreshEvery(t *testing.T) {
	for in, want := range map[string]time.Duration{"6h": 6 * time.Hour, "1d": 24 * time.Hour, "30m": 30 * time.Minute, "off": 0, "": 0, " 0 ": 0, "1.5d": 36 * time.Hour} {
		if got, err := ParseRefreshEvery(in); err != nil || got != want {
			t.Errorf("ParseRefreshEvery(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"5m", "soon", "1x", "-1h"} {
		if _, err := ParseRefreshEvery(bad); err == nil {
			t.Errorf("ParseRefreshEvery(%q) took it", bad)
		}
	}
}

// The gateway fetches a list with a RefreshEvery once it is that old, and
// not before; a fetch that fails is tried again after listRetry, not at
// every look; one switched off isn't asked; and a newly listed model a
// rule matches is taken on the way (#2135).
func TestKeepListsFreshFetchesWhatIsDue(t *testing.T) {
	autolistHome(t)
	r := newRelayList(t, "gpt-6")
	if err := Save(Provider{ID: "relay", Name: "Relay", Chat: r.srv.URL + "/v1", Key: "sk-relay", Models: []string{"gpt-6"}}); err != nil {
		t.Fatal(err)
	}
	other := newRelayList(t, "x-1")
	if err := Save(Provider{ID: "plain", Name: "Plain", Chat: other.srv.URL + "/v1", Key: "sk-plain"}); err != nil {
		t.Fatal(err)
	}
	if err := SetRefreshEvery("relay", "6h"); err != nil {
		t.Fatal(err)
	}
	if _, err := SetAutoInclude("relay", []string{"gpt-*"}); err != nil {
		t.Fatal(err)
	}
	if err := SetRefreshEvery("relay", "5m"); err == nil {
		t.Fatal("a list fetched every 5 minutes was taken")
	}
	t.Cleanup(func() {
		listsTried.Lock()
		clear(listsTried.m)
		listsTried.Unlock()
	})
	now := time.Now()
	// never fetched: due at once; the provider with no schedule isn't
	if got := refreshDueLists(context.Background(), now); !slices.Equal(got, []string{"relay"}) {
		t.Fatalf("asked %v, want [relay]", got)
	}
	if r.asked.Load() != 1 || other.asked.Load() != 0 {
		t.Fatalf("lists asked: relay %d, plain %d", r.asked.Load(), other.asked.Load())
	}
	// fetched just now: not due for 6 hours
	r.set("gpt-7", "gpt-6", "llama-5")
	built := AllBuilt.Load()
	if got := refreshDueLists(context.Background(), now.Add(5*time.Hour)); len(got) != 0 {
		t.Fatalf("asked %v before its 6 hours", got)
	}
	// a look with nothing due asks no agent for its sign-in: All reads
	// each one's, the Claude keychain's `security` among them (#3185)
	if n := AllBuilt.Load() - built; n != 0 {
		t.Fatalf("a look with nothing due built All %d times", n)
	}
	got := refreshDueLists(context.Background(), now.Add(6*time.Hour+time.Minute))
	if !slices.Equal(got, []string{"relay"}) || r.asked.Load() != 2 {
		t.Fatalf("asked %v (%d times) after its 6 hours", got, r.asked.Load())
	}
	if ids := servedIDs(t, "relay"); !slices.Equal(ids, []string{"gpt-7", "gpt-6"}) {
		t.Fatalf("exposed after the scheduled fetch: %v", ids)
	}
	if ms, _, _ := catalog.Live("relay"); len(ms) != 3 {
		t.Fatalf("the list kept: %v", ms)
	}

	// a vendor that fails is asked again after listRetry, not each minute
	r.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.asked.Add(1)
		http.Error(w, "down", http.StatusBadGateway)
	})
	late := now.Add(13 * time.Hour)
	refreshDueLists(context.Background(), late)
	n := r.asked.Load()
	if got := refreshDueLists(context.Background(), late.Add(time.Minute)); len(got) != 0 || r.asked.Load() != n {
		t.Fatalf("a list that failed a minute ago was asked again: %v", got)
	}
	if got := refreshDueLists(context.Background(), late.Add(listRetry)); !slices.Equal(got, []string{"relay"}) {
		t.Fatalf("a list that failed %s ago wasn't asked again: %v", listRetry, got)
	}

	// switched off: not asked
	if err := SetOff("relay", true); err != nil {
		t.Fatal(err)
	}
	if got := refreshDueLists(context.Background(), late.Add(2*listRetry)); len(got) != 0 {
		t.Fatalf("a provider switched off was asked: %v", got)
	}
}

// A save that doesn't set them keeps the schedule and the rules: the
// window's editor and `uni-switch provider set` change other things.
func TestSaveKeepsTheListsScheduleAndRules(t *testing.T) {
	autolistHome(t)
	r := newRelayList(t, "gpt-6")
	if err := Save(Provider{ID: "relay", Name: "Relay", Chat: r.srv.URL + "/v1", Key: "sk-relay"}); err != nil {
		t.Fatal(err)
	}
	if err := SetRefreshEvery("relay", "1d"); err != nil {
		t.Fatal(err)
	}
	if _, err := SetAutoInclude("relay", []string{"gpt-*"}); err != nil {
		t.Fatal(err)
	}
	p, _ := Find("relay")
	p.Name = "My Relay"
	if err := Save(*p); err != nil {
		t.Fatal(err)
	}
	p, _ = Find("relay")
	if p.RefreshEvery != "1d" || !slices.Equal(p.AutoInclude, []string{"gpt-*"}) || p.RefreshInterval() != 24*time.Hour {
		t.Fatalf("after a save: every %q, rules %v", p.RefreshEvery, p.AutoInclude)
	}
	if err := SetRefreshEvery("relay", "off"); err != nil {
		t.Fatal(err)
	}
	if p, _ := Find("relay"); p.RefreshEvery != "" || p.RefreshInterval() != 0 {
		t.Fatalf("off left %q", p.RefreshEvery)
	}
}

// A Remote uni-switch's list follows the other uni-switch's with no schedule set
// (6igsteven on Discord, 10-24): a model listed there later, ZCode's
// GLM-5.3-Trial once its plugin read the gift, is served here after
// RemoteUniSwitchEvery, not only after Fetch models in the editor. A relay
// with no schedule is still asked only when told, and a Remote uni-switch its
// user set off is too.
