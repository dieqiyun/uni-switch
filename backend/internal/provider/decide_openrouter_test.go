package provider

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// fakeOpenRouter answers for openrouter.ai: its chat models at /models,
// Jev Router among them, its decision models at
// /models?output_modalities=decisions, and System One at /systemone.
func fakeOpenRouter(t *testing.T) (asked func() []string, lists func() int) {
	t.Helper()
	var mu sync.Mutex
	var models []string
	decisions := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/models":
			if r.URL.Query().Get("output_modalities") == "decisions" {
				mu.Lock()
				decisions++
				mu.Unlock()
				w.Write([]byte(`{"data":[{"id":"perplexity/pplx-decider-v1-27b","context_length":262144,"architecture":{"input_modalities":["text","image"],"output_modalities":["decisions"]}},{"id":"liquid/d1","context_length":65536,"architecture":{"input_modalities":["text"]}},{"id":"~typesafe/jev-latest","context_length":32000,"architecture":{"input_modalities":["text"]}}]}`))
				return
			}
			w.Write([]byte(`{"data":[{"id":"openai/gpt-5","context_length":400000},{"id":"typesafe/jev-router","context_length":1000000}]}`))
		case "/api/v1/systemone":
			var q struct{ Model string }
			json.NewDecoder(r.Body).Decode(&q)
			mu.Lock()
			models = append(models, q.Model)
			mu.Unlock()
			w.Write([]byte(`{"answers":{"ok":{"type":"noul","noul":0.9}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().String()
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, addr)
		},
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = tr
	t.Cleanup(func() { http.DefaultClient.Transport = old })
	return func() []string {
			mu.Lock()
			defer mu.Unlock()
			return slices.Clone(models)
		}, func() int {
			mu.Lock()
			defer mu.Unlock()
			return decisions
		}
}

// OpenRouter's decision models (ARNO on Discord: the OpenRouter
// provider's list didn't have them): they are listed apart from its chat
// models, so the built-in OpenRouter fetches that list too and offers them
// to routing groups, each with its window and input, asked at /systemone
// with the same key. Its Jev Router, a chat model named like Jev, stays a
// model agents talk to.
func TestOpenRouterListsDecisionModels(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	asked, _ := fakeOpenRouter(t)

	// an OpenRouter provider saved before its preset had decisions
	if err := Save(Provider{ID: "openrouter", Name: "OpenRouter", Preset: "openrouter", Key: "k",
		Chat: "https://openrouter.ai/api/v1", Models: []string{"openai/gpt-5", "typesafe/jev-router"}}); err != nil {
		t.Fatal(err)
	}
	p, err := Find("openrouter")
	if err != nil || !p.Decides() || p.DecideOnly() {
		t.Fatalf("OpenRouter has no decision API: %+v", p)
	}
	if _, err := p.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, _ = Find("openrouter")
	if p == nil {
		t.Fatal("gone")
	}

	if p.DecidesModel("typesafe/jev-router") || !p.DecidesModel("liquid/d1") || !p.DecidesModel("~typesafe/jev-latest") {
		t.Fatal("decision models are not OpenRouter's decision list")
	}
	var deciders []string
	for _, e := range Deciders() {
		deciders = append(deciders, e.ID)
	}
	if !slices.Equal(deciders, []string{"openrouter/perplexity/pplx-decider-v1-27b", "openrouter/liquid/d1", "openrouter/~typesafe/jev-latest"}) {
		t.Fatalf("deciders: %v", deciders)
	}
	var agents []string
	for _, e := range providerEntries() {
		agents = append(agents, e.Model)
	}
	if !slices.Contains(agents, "typesafe/jev-router") || !slices.Contains(agents, "openai/gpt-5") || slices.Contains(agents, "liquid/d1") {
		t.Fatalf("agents' models: %v", agents)
	}

	facts := map[string][2]any{}
	for _, m := range p.DecisionModels() {
		facts[m.ID] = [2]any{ListedWindow(m), m.ImageInput != nil && *m.ImageInput}
	}
	if facts["perplexity/pplx-decider-v1-27b"] != [2]any{262144, true} || facts["liquid/d1"] != [2]any{65536, false} {
		t.Fatalf("facts: %v", facts)
	}

	// a group classified by one is asked at OpenRouter's /systemone
	q, m, err := RouteDecider("openrouter/liquid/d1")
	if err != nil || q.ID != "openrouter" || m != "liquid/d1" {
		t.Fatalf("routed to %s %s: %v", q.ID, m, err)
	}
	if err := q.AskSystemOne(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if _, m, _ := RouteDecider("openrouter"); m != "~typesafe/jev-latest" {
		t.Fatalf("OpenRouter's Jev: %s", m)
	}
	if got := asked(); !slices.Equal(got, []string{"liquid/d1"}) {
		t.Fatalf("asked: %v", got)
	}
}

// A System One provider at OpenRouter added by hand, with no list URL,
// gets OpenRouter's decision models, not Jev Router alone; and one whose
// list was fetched before uni-switch kept windows and input is fetched again
// when lists are (ARNO: they showed neither).
func TestOpenRouterDecisionListRefreshed(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	_, lists := fakeOpenRouter(t)

	bare := Provider{ID: "or-s1", Name: "OpenRouter S1", Key: "k", Decide: "https://openrouter.ai/api/v1"}
	if err := Save(bare); err != nil {
		t.Fatal(err)
	}
	ms, err := bare.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	if !slices.Equal(ids, []string{"perplexity/pplx-decider-v1-27b", "liquid/d1", "~typesafe/jev-latest"}) {
		t.Fatalf("decision models: %v", ids)
	}

	old := Provider{ID: "or-s2", Name: "OpenRouter S2", Key: "k", Decide: "https://openrouter.ai/api/v1",
		ModelsURL: "https://openrouter.ai/api/v1/models?output_modalities=decisions"}
	if err := Save(old); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("or-s2", old.Decide, []catalog.Model{{ID: "perplexity/pplx-decider-v1-27b"}, {ID: "liquid/d1"}}); err != nil {
		t.Fatal(err)
	}
	before := lists()
	FetchNew(10 * time.Second)
	if lists() == before {
		t.Fatal("the old list wasn't fetched again")
	}
	p, err := Find("or-s2")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range p.Available() {
		if m.ID == "perplexity/pplx-decider-v1-27b" && (ListedWindow(m) != 262144 || !m.Images) {
			t.Fatalf("pplx-decider: %+v", m)
		}
	}
	// fetched now, it isn't fetched again at the next start
	before = lists()
	newFetches.Lock()
	clear(newFetches.m)
	newFetches.Unlock()
	FetchNew(10 * time.Second)
	if lists() != before {
		t.Fatal("a list with its facts was fetched again")
	}
}

// A routing group's pickers offer an OpenRouter provider's decision models
// as its picks say (coeo91 on Discord: two free models picked, and a new
// group's picker listed OpenRouter's decision models after them, which the
// provider's editor showed unpicked). With none picked every one is
// offered; one a group names already is offered whatever the picks; and
// the gateway still answers each, picked or not.
func TestOpenRouterDecidersOfferedAsPicked(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fakeOpenRouter(t)

	ids := func(es []Entry) []string {
		var out []string
		for _, e := range es {
			out = append(out, e.ID)
		}
		return out
	}
	all := []string{"openrouter/perplexity/pplx-decider-v1-27b", "openrouter/liquid/d1", "openrouter/~typesafe/jev-latest"}
	save := func(picks ...string) {
		t.Helper()
		if err := Save(Provider{ID: "openrouter", Name: "OpenRouter", Preset: "openrouter", Key: "k",
			Chat: "https://openrouter.ai/api/v1", Decide: "https://openrouter.ai/api/v1", Models: picks}); err != nil {
			t.Fatal(err)
		}
	}
	save()
	p, err := Find("openrouter")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}

	// none picked: every decision model, as before
	if got := ids(OfferedDeciders(nil)); !slices.Equal(got, all) {
		t.Fatalf("none picked, offered: %v", got)
	}

	// two chat models picked: no decision model is offered, while the
	// gateway still lists and answers each
	save("openai/gpt-5", "typesafe/jev-router")
	if got := ids(OfferedDeciders(nil)); len(got) != 0 {
		t.Fatalf("chat models picked, offered: %v", got)
	}
	if got := ids(Deciders()); !slices.Equal(got, all) {
		t.Fatalf("the gateway's: %v", got)
	}
	if _, m, err := RouteDecider("openrouter/liquid/d1"); err != nil || m != "liquid/d1" {
		t.Fatalf("an unpicked decision model isn't answered: %s %v", m, err)
	}

	// one a group names is offered still, the rest not
	named := func(id string) bool { return id == "openrouter/~typesafe/jev-latest" }
	if got := ids(OfferedDeciders(named)); !slices.Equal(got, []string{"openrouter/~typesafe/jev-latest"}) {
		t.Fatalf("named by a group, offered: %v", got)
	}

	// one decision model picked beside a chat model: that one alone
	save("openai/gpt-5", "liquid/d1")
	if got := ids(OfferedDeciders(nil)); !slices.Equal(got, []string{"openrouter/liquid/d1"}) {
		t.Fatalf("liquid/d1 picked, offered: %v", got)
	}
	// and agents are still not offered it as a chat model
	for _, e := range providerEntries() {
		if e.Model == "liquid/d1" {
			t.Fatal("a picked decision model is offered to agents")
		}
	}
}
