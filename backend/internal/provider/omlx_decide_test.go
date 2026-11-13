package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
)

// fakeOMLX answers as oMLX 0.7.1.dev1 does (jundot/omlx server.py,
// engine_pool.get_status): /v1/models lists every model with nothing to
// tell a decision model from a chat one, by its alias where it has one;
// /v1/models/status gives each one's model_type; /v1/systemone answers
// System One. Its decision models: Clef-Flash by its folder's name, and
// OpenJev under an alias that doesn't say what it is; one more is hidden.
func fakeOMLX(t *testing.T) (base string, asked func() []string) {
	t.Helper()
	var mu sync.Mutex
	var models []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Write([]byte(`{"object":"list","data":[{"id":"Qwen3.8-27B-oQ4e","object":"model","owned_by":"omlx","max_model_len":262144},{"id":"clef-flash-oQ4e","object":"model","owned_by":"omlx","max_model_len":65536},{"id":"router","object":"model","owned_by":"omlx","max_model_len":32768}]}`))
		case "/v1/models/status":
			w.Write([]byte(`{"final_ceiling":0,"current_model_memory":0,"model_count":4,"loaded_count":0,"models":[` +
				`{"id":"Qwen3.8-27B-oQ4e","model_path":"/m/Qwen3.8-27B-oQ4e","loaded":false,"engine_type":"batched","model_type":"llm","config_model_type":"qwen3_5","is_helper":false,"max_context_window":262144,"max_tokens":32768,"is_favorite":false,"is_hidden":false},` +
				`{"id":"clef-flash-oQ4e","model_path":"/m/clef-flash-oQ4e","loaded":false,"engine_type":"decision","model_type":"decision","config_model_type":"qwen3_5","is_helper":false,"max_context_window":65536,"max_tokens":32768,"is_favorite":false,"is_hidden":false},` +
				`{"id":"OpenJev-27B-4bit","model_path":"/m/OpenJev-27B-4bit","loaded":false,"engine_type":"decision","model_type":"decision","config_model_type":"qwen3_5","is_helper":false,"model_alias":"router","max_context_window":32768,"max_tokens":32768,"is_favorite":false,"is_hidden":false},` +
				`{"id":"clef-oQ4e","model_path":"/m/clef-oQ4e","loaded":false,"engine_type":"decision","model_type":"decision","config_model_type":"qwen3_5","is_helper":false,"max_context_window":65536,"max_tokens":32768,"is_favorite":false,"is_hidden":true}]}`))
		case "/v1/systemone":
			var q struct{ Model string }
			json.NewDecoder(r.Body).Decode(&q)
			mu.Lock()
			models = append(models, q.Model)
			mu.Unlock()
			w.Write([]byte(`{"model":"` + q.Model + `","answers":{"ok":{"type":"noul","noul":0.9}},"usage":{"input_tokens":12,"output_tokens":0}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(models)
	}
}

// oMLX's decision models (#1641, ObsidianArch02): an oMLX provider asks
// System One at its /v1/systemone, and its decision models are the ones
// its server says are, by its model status, whatever they are named; they
// classify routing groups and are no models agents talk to.
func TestOMLXDecisionModels(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	base, asked := fakeOMLX(t)

	// an oMLX provider saved before its preset had System One, at a port
	// of the user's own
	if err := Save(Provider{ID: "omlx", Name: "oMLX", Preset: OMLXPreset,
		Chat: base + "/v1", Responses: base + "/v1", Anthropic: base}); err != nil {
		t.Fatal(err)
	}
	p, err := Find("omlx")
	if err != nil || !p.Decides() || p.DecideOnly() || p.Decide != base+"/v1" {
		t.Fatalf("oMLX has no decision API at its own server: %+v", p)
	}
	if _, err := p.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, _ = Find("omlx")

	if !p.DecidesModel("clef-flash-oQ4e") || !p.DecidesModel("router") || p.DecidesModel("Qwen3.8-27B-oQ4e") || p.DecidesModel("clef-oQ4e") {
		t.Fatal("decision models are not the ones oMLX says are")
	}
	var deciders []string
	for _, e := range Deciders() {
		deciders = append(deciders, e.ID)
	}
	if !slices.Equal(deciders, []string{"omlx/clef-flash-oQ4e", "omlx/router"}) {
		t.Fatalf("deciders: %v", deciders)
	}
	var agents []string
	for _, e := range providerEntries() {
		agents = append(agents, e.Model)
	}
	if !slices.Equal(agents, []string{"Qwen3.8-27B-oQ4e"}) {
		t.Fatalf("agents' models: %v", agents)
	}
	for _, m := range p.DecisionModels() {
		if m.ID == "clef-flash-oQ4e" && (ListedWindow(m) != 65536 || m.ImageInput == nil || !*m.ImageInput) {
			t.Fatalf("Clef-Flash: %+v", m)
		}
	}

	q, m, err := RouteDecider("omlx/router")
	if err != nil || q.ID != "omlx" || m != "router" {
		t.Fatalf("routed to %s %s: %v", q.ID, m, err)
	}
	if err := q.AskSystemOne(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if _, m, _ := RouteDecider("omlx"); m != "clef-flash-oQ4e" {
		t.Fatalf("oMLX's default decision model: %q", m)
	}
	if got := asked(); !slices.Equal(got, []string{"router"}) {
		t.Fatalf("asked: %v", got)
	}
}

// An oMLX server with no decision model, an older one among them, has
// none: no Jev is made up for it, and its chat models stay chat models.
func TestOMLXWithoutDecisionModels(t *testing.T) {
	ms, err := listedOMLXDecide([]byte(`{"models":[{"id":"Qwen3.8-27B-oQ4e","engine_type":"batched","model_type":"llm","is_hidden":false}]}`))
	if err != nil || len(ms) != 0 {
		t.Fatalf("%v %v", ms, err)
	}
	if _, err := listedOMLXDecide([]byte(`{"detail":"Not Found"}`)); err == nil {
		t.Fatal("a 404's body read as a list")
	}
	p := Provider{ID: "omlx-x", Preset: OMLXPreset, Chat: "http://127.0.0.1:1/v1", Decide: "http://127.0.0.1:1/v1"}
	if got := p.decideListed(); len(got) != 0 {
		t.Fatalf("made up: %v", got)
	}
	if p.Jev() != "" || p.DecidesModel("Qwen3.8-27B-oQ4e") {
		t.Fatalf("Jev: %q", p.Jev())
	}
}
