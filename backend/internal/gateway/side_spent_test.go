package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// restAs rests ids (a provider, a key, or a provider's model) as routing
// does after the vendor said why, for an hour.
func restAs(t *testing.T, why string, ids ...string) {
	t.Helper()
	restingUntil.Lock()
	defer restingUntil.Unlock()
	for _, id := range ids {
		restingUntil.m[id] = time.Now().Add(time.Hour)
		restingUntil.note[id] = Rest{Why: why, Status: 429, By: "window"}
	}
	t.Cleanup(func() {
		restingUntil.Lock()
		defer restingUntil.Unlock()
		for _, id := range ids {
			delete(restingUntil.m, id)
			delete(restingUntil.note, id)
		}
	})
}

// Image recognition left to uni-switch picks a describer with allowance left
// over one out of it, whatever their order: the cheaper key's describer
// out of quota, resting after a refusal or read used up alike, gives way
// to the dearer one (#1801, hibaliqya-yonma: each image went on to an
// account at 100% and was refused 429). A rate limit isn't out of
// allowance. With every one out, one is still named, so agents are still
// told images are described.
func TestAutoVisionPassesOverOneOut(t *testing.T) {
	fresh(t)
	for id, price := range map[string]float64{"cheap": 1, "dear": 5} {
		if err := provider.Save(provider.Provider{ID: id, Name: id, Chat: "http://" + id + ".invalid/v1", Key: id + "-key", Models: []string{"eye"}}); err != nil {
			t.Fatal(err)
		}
		if err := catalog.SaveLive(id, "http://"+id+".invalid/v1", []catalog.Model{
			{ID: "eye", Images: true, ImageInput: imageInputBool(true), Price: &catalog.Price{Input: price, Output: price}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	windows := map[string]provider.Allowance{}
	keyWindows(t, windows)
	if m := AutoVision(); m != "cheap/eye" {
		t.Fatalf("AutoVision = %q, want the cheaper", m)
	}
	restAs(t, failRate, "cheap")
	if m := AutoVision(); m != "cheap/eye" {
		t.Fatalf("AutoVision = %q with cheap only rate limited", m)
	}
	restAs(t, failQuota, "cheap")
	if m := AutoVision(); m != "dear/eye" {
		t.Fatalf("AutoVision = %q with cheap out of quota", m)
	}
	restAs(t, failCredit, "dear")
	if m := AutoVision(); m != "cheap/eye" || !provider.Described() {
		t.Fatalf("AutoVision = %q, described %v, with every one out", m, provider.Described())
	}
	if _, ready := autoVision(); ready {
		t.Fatal("a pick out of allowance said to be ready")
	}

	// used up as last read, before any refusal
	restingUntil.Lock()
	restingUntil.m = map[string]time.Time{}
	restingUntil.Unlock()
	windows["cheap-key"] = week(100, time.Now().Add(time.Hour))
	if m := AutoVision(); m != "dear/eye" {
		t.Fatalf("AutoVision = %q with cheap's week used up", m)
	}
	windows["cheap-key"] = week(100, time.Now().Add(-time.Hour))
	if m := AutoVision(); m != "cheap/eye" {
		t.Fatalf("AutoVision = %q after cheap's week renewed", m)
	}

	// out for one model only: the same provider's other one that sees
	p, err := provider.Find("cheap")
	if err != nil {
		t.Fatal(err)
	}
	p.Models = append(p.Models, "other-eye")
	if err := provider.Save(*p); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive(p.ID, p.Chat, []catalog.Model{
		{ID: "eye", Images: true, Price: &catalog.Price{Input: 1, Output: 1}},
		{ID: "other-eye", Images: true, Price: &catalog.Price{Input: 2, Output: 2}},
	}); err != nil {
		t.Fatal(err)
	}
	restAs(t, failQuota, "cheap/eye")
	if m := AutoVision(); m != "cheap/other-eye" {
		t.Fatalf("AutoVision = %q with only cheap/eye out", m)
	}
}

// The tier is kept among describers with allowance: a Claude account goes
// before an API key, but not once its five hours are used up.
func TestAutoVisionTierAmongThoseWithAllowance(t *testing.T) {
	codexSignedIn(t)
	used := map[string]float64{"me@example.com": 20}
	capUsage(t, used)
	if err := provider.Save(provider.Provider{ID: "keyed", Name: "keyed", Chat: "http://keyed.invalid/v1", Key: "k", Models: []string{"eye"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("keyed", "http://keyed.invalid/v1", []catalog.Model{{ID: "eye", Images: true, ImageInput: imageInputBool(true)}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("codex", provider.CodexBase, []catalog.Model{{ID: "gpt-6.1-sol", Images: true, ImageInput: imageInputBool(true)}}); err != nil {
		t.Fatal(err)
	}
	if got := AutoVision(); got != "codex/gpt-6.1-sol" {
		t.Fatalf("AutoVision = %q, want the Codex account's first", got)
	}
	used["me@example.com"] = 100
	if got := AutoVision(); got != "keyed/eye" {
		t.Fatalf("AutoVision = %q with the Codex account's five hours used up", got)
	}
}

// spentModel is out only when every account routing would try is: a
// spare with room keeps the model, and a pool of one model's own leaves
// the account's others in theirs.
func TestSpentModelAcrossAccounts(t *testing.T) {
	codexSignedIn(t, "spare@example.com")
	used := map[string]float64{"me@example.com": 100, "spare@example.com": 20}
	capUsage(t, used)
	p, err := provider.Find("codex")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, spent := spentModel(*p, "gpt-6.1-sol", provider.Chat, now); spent {
		t.Fatal("a spare account has allowance")
	}
	used["spare@example.com"] = 100
	if back, spent := spentModel(*p, "gpt-6.1-sol", provider.Chat, now); !spent || back.IsZero() {
		t.Fatalf("both accounts are out: %v %v", spent, back)
	}

	claude := provider.Provider{ID: "claude", Account: &provider.Account{Agent: "claude", User: "me@example.com"}}
	if _, spent := spentModel(claude, "claude-haiku-5-5", provider.Chat, now); !spent {
		t.Fatal("Haiku kept with Claude's five hours used up")
	}
	allowances = func(string) map[string]provider.Allowance {
		return map[string]provider.Allowance{"me@example.com": {{Used: 100, Resets: now.Add(time.Hour), Model: "opus"}}}
	}
	if _, spent := spentModel(claude, "claude-haiku-5-5", provider.Chat, now); spent {
		t.Fatal("Opus's own pool left Haiku out")
	}
	if _, spent := spentModel(claude, "claude-opus-5", provider.Chat, now); !spent {
		t.Fatal("Opus kept with its own pool used up")
	}
}

// With every describer out of allowance, an image isn't sent to uni-switch's
// pick to be refused again: the request is turned away saying so, on the
// gateway and the vision MCP alike, until the window renews. A describer
// the Settings name is asked as before.
func TestVisionDoesNotAskSpentDescriber(t *testing.T) {
	s, u := eyed(t)
	windows := map[string]provider.Allowance{"key": week(100, time.Now().Add(time.Hour))}
	keyWindows(t, windows)
	body := `{"model":"probe/text","messages":[{"role":"user","content":[{"type":"text","text":"read"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8="}}]}]}`
	code, out := postAs(t, s, "", body)
	if code < 400 || !strings.Contains(out, "out of its allowance") {
		t.Fatalf("spent describer: %d %s", code, out)
	}
	m := readMCP(t, visionPost(localGuard(s.Handler()), "127.0.0.1:50000", analyzeCall("data:image/png;base64,aGVsbG8=", "")))
	if !m.Result.IsError || !strings.Contains(m.Result.Content[0].Text, "out of its allowance") {
		t.Fatalf("spent MCP describer: %+v", m)
	}
	if n := len(u.sent("eye")); n != 0 {
		t.Fatalf("the describer out of allowance was asked %d times", n)
	}
	windows["key"] = week(100, time.Now().Add(-time.Hour))
	if code, out := postAs(t, s, "", body); code != 200 || len(u.sent("eye")) != 1 {
		t.Fatalf("renewed describer: %d %s, asked %d", code, out, len(u.sent("eye")))
	}

	// named in Settings: asked as the user picked it
	windows["key"] = week(100, time.Now().Add(time.Hour))
	st := settings.Load()
	st.Vision = "probe/eye"
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	postAs(t, s, "", strings.Replace(body, "aGVsbG8=", "d29ybGQ=", 1))
	if n := len(u.sent("eye")); n != 2 {
		t.Fatalf("the describer named in Settings was asked %d times, want 2", n)
	}
}

// searchFake is an Anthropic API that searches, answering each key with
// what it found; the keys it was asked with are kept.
type searchFake struct {
	mu    sync.Mutex
	asked []string
}

func (f *searchFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body)
	f.mu.Lock()
	f.asked = append(f.asked, r.Header.Get("X-Api-Key"))
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-5-5","content":[{"type":"text","text":"Go 1.27.1 is out: https://go.dev/doc/devel/release"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
}

func (f *searchFake) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.asked)
}

// The web search uni-switch runs for a model that can't is a side request of
// the same kind: the searcher it picks has allowance left, and one out of
// it isn't asked, the next searcher or the search APIs searching instead.
func TestSearcherPassesOverOneOut(t *testing.T) {
	fresh(t)
	f := &searchFake{}
	up := httptest.NewServer(f)
	t.Cleanup(up.Close)
	hosts := searchHosts[provider.Anthropic]
	searchHosts[provider.Anthropic] = append(slices.Clone(hosts), provider.HostOf(up.URL))
	t.Cleanup(func() { searchHosts[provider.Anthropic] = hosts })
	for _, id := range []string{"first", "second"} {
		if err := provider.Save(provider.Provider{ID: id, Name: id, Anthropic: up.URL, Key: id + "-key", Models: []string{"claude-haiku-5-5"}}); err != nil {
			t.Fatal(err)
		}
		if err := catalog.SaveLive(id, up.URL, []catalog.Model{{ID: "claude-haiku-5-5"}}); err != nil {
			t.Fatal(err)
		}
	}
	windows := map[string]provider.Allowance{}
	keyWindows(t, windows)
	first, _, ok := autoSearcher()
	if !ok {
		t.Fatal("no searcher")
	}
	other := "second"
	if first.ID == "second" {
		other = "first"
	}
	windows[first.ID+"-key"] = week(100, time.Now().Add(time.Hour))
	if p, _, ok := autoSearcher(); !ok || p.ID != other {
		t.Fatalf("autoSearcher = %q with %s used up", p.ID, first.ID)
	}
	said, _, err := New().webSearch(context.Background(), "latest go")
	if err != nil || !strings.Contains(said, "Go 1.27.1") {
		t.Fatalf("search: %q %v", said, err)
	}
	if got := f.keys(); slices.Contains(got, first.ID+"-key") || !slices.Contains(got, other+"-key") {
		t.Fatalf("asked with %q", got)
	}

	// both out: uni-switch still offers its search, and asks neither
	windows[other+"-key"] = week(100, time.Now().Add(time.Hour))
	if !canSearch() {
		t.Fatal("search no longer offered with its searchers out for now")
	}
	before := len(f.keys())
	if _, _, err := New().webSearch(context.Background(), "latest go"); err == nil || !strings.Contains(err.Error(), "out of its allowance") {
		t.Fatalf("both out: %v", err)
	}
	if n := len(f.keys()); n != before {
		t.Fatalf("a searcher out of allowance was asked: %q", f.keys()[before:])
	}
}
