package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/codexcat"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// terraEntry is one model as a relay built for Codex (sub2api) lists it
// when asked with Codex's client_version: Codex's own catalog shape
// (codex-rs ModelInfo), with values uni-switch never writes itself, so an
// entry made up by uni-switch can't pass for it.
const terraEntry = `{"slug":"gpt-6.1-terra","display_name":"GPT-6.1 Terra","description":"Frontier agentic coding model.",` +
	`"default_reasoning_level":"medium","supported_reasoning_levels":[` +
	`{"effort":"low","description":"Fast responses with lighter reasoning"},` +
	`{"effort":"medium","description":"Balances speed and reasoning depth for everyday tasks"},` +
	`{"effort":"high","description":"Greater reasoning depth for complex problems"},` +
	`{"effort":"xhigh","description":"Extra high reasoning depth for complex problems"}],` +
	`"shell_type":"shell_command","visibility":"list","supported_in_api":true,"priority":3,` +
	`"availability_nux":null,"upgrade":null,` +
	`"base_instructions":"You are Codex, based on GPT-6.1 Terra. You are running as a coding agent in the Codex CLI on a user's computer.",` +
	`"model_messages":{"instructions_template":"You are Codex, a coding agent based on GPT-6.1 Terra.\n\n{{ personality }}","instructions_variables":{"personality_default":"","personality_friendly":"You optimize for team morale.","personality_pragmatic":"You are a deeply pragmatic engineer."}},` +
	`"supports_reasoning_summaries":true,"default_reasoning_summary":"none","support_verbosity":true,"default_verbosity":"low",` +
	`"apply_patch_tool_type":"freeform","web_search_tool_type":"text_and_image","truncation_policy":{"mode":"tokens","limit":12000},` +
	`"supports_parallel_tool_calls":true,"supports_image_detail_original":true,"context_window":272000,"max_context_window":1000000,` +
	`"auto_compact_token_limit":null,"effective_context_window_percent":95,"experimental_supported_tools":[],` +
	`"input_modalities":["text","image"],"prefer_websockets":true,"supports_search_tool":true,` +
	`"service_tiers":[{"id":"priority","name":"Fast","description":"1.5x faster, more usage"}]}`

// relayList is a fake sub2api: /v1/models lists plainly (id, owner,
// display name), and in Codex's shape when asked with client_version, as
// sub2api's modelsHandler does. manifest is what the second answers.
type relayList struct {
	manifest atomic.Value // string; "" answers 404
	plain    atomic.Value // string
	asked    atomic.Int32 // client_version asks
}

func newRelay(t *testing.T) (*relayList, string) {
	r := &relayList{}
	r.manifest.Store(`{"models":[` + terraEntry + `]}`)
	r.plain.Store(`{"object":"list","data":[` +
		`{"id":"gpt-6.1-terra","object":"model","created":1798761600,"owned_by":"openai","display_name":"GPT-6.1 Terra"},` +
		`{"id":"glm-5","object":"model","created":1798761600,"owned_by":"zhipu","display_name":"GLM-5"}]}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/models" {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Query().Get("client_version") != "" {
			r.asked.Add(1)
			m, _ := r.manifest.Load().(string)
			if m == "" {
				w.WriteHeader(http.StatusNotFound)
				io.WriteString(w, `{"error":{"message":"not found"}}`)
				return
			}
			io.WriteString(w, m)
			return
		}
		io.WriteString(w, r.plain.Load().(string))
	}))
	t.Cleanup(srv.Close)
	return r, srv.URL + "/v1"
}

// codexEntry is the entry of id in the catalog a Codex naming uni-switch its
// model_provider reads (/v1/codex/models).
func codexEntry(t *testing.T, id string) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, httptest.NewRequest("GET", CodexCatalogPath+"?client_version=0.164.0", nil))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var list struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	for _, e := range list.Models {
		if e["slug"] == id {
			return e
		}
	}
	t.Fatalf("no %s in %s", id, rec.Body)
	return nil
}

func fetchRelay(t *testing.T) {
	t.Helper()
	p, err := provider.Find("relay")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// A provider the user added by its address, a relay that lists its models
// in Codex's own shape when asked as Codex asks (sub2api, ctopsc on
// Discord), has each model's entry handed to Codex as the relay gave it —
// its instructions, window, levels, tools and tiers — and not one uni-switch
// made up from models.dev's guess for the id. The user's own window and
// levels still win, a model the shaped list leaves out is made as before,
// and a shaped list that can't be read keeps what the last one said.
func TestCodexListedEntry(t *testing.T) {
	fresh(t)
	relay, base := newRelay(t)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Models: []string{"gpt-6.1-terra", "glm-5"}, Responses: base}); err != nil {
		t.Fatal(err)
	}
	fetchRelay(t)
	if n := relay.asked.Load(); n != 1 {
		t.Fatalf("asked with client_version %d times on one fetch, want 1", n)
	}
	var upstream map[string]any
	json.Unmarshal([]byte(terraEntry), &upstream)
	verbatim := func(t *testing.T, e map[string]any, except ...string) {
		t.Helper()
		for k, v := range upstream {
			switch k {
			case "slug", "display_name", "priority", "description", "availability_nux", "upgrade":
				continue // uni-switch's id, name and place; the notices name slugs it doesn't have
			}
			if strings.Contains(strings.Join(except, ","), k) {
				continue
			}
			if !reflect.DeepEqual(e[k], v) {
				t.Errorf("%s = %v, the relay said %v", k, e[k], v)
			}
		}
	}

	t.Run("the relay's entry", func(t *testing.T) {
		e := codexEntry(t, "relay/gpt-6.1-terra")
		verbatim(t, e)
		if d, _ := e["description"].(string); d != "Frontier agentic coding model. via uni-switch" {
			t.Errorf("description %q, want the relay's with via uni-switch", d)
		}
		// the one it doesn't list in Codex's shape is made as before
		g := codexEntry(t, "relay/glm-5")
		if g["base_instructions"] != codexcat.Prompt {
			t.Errorf("glm-5's instructions aren't uni-switch's own: %.80v", g["base_instructions"])
		}
	})

	t.Run("a shaped list that can't be read keeps the last", func(t *testing.T) {
		relay.manifest.Store("")
		fetchRelay(t)
		verbatim(t, codexEntry(t, "relay/gpt-6.1-terra"))
		// nor does one with no model in it (an odd shape)
		relay.manifest.Store(`{"models":"none"}`)
		fetchRelay(t)
		verbatim(t, codexEntry(t, "relay/gpt-6.1-terra"))
		// one that answers plainly (a relay that ignores client_version)
		// says it has none in Codex's shape
		relay.manifest.Store(relay.plain.Load())
		fetchRelay(t)
		if e := codexEntry(t, "relay/gpt-6.1-terra"); e["base_instructions"] != codexcat.Official("gpt-6.1-terra") {
			t.Errorf("an entry Codex can't read was kept: %.80v", e["base_instructions"])
		}
		relay.manifest.Store(`{"models":[` + terraEntry + `]}`)
		fetchRelay(t)
	})

	t.Run("the user's window and levels win", func(t *testing.T) {
		p, _ := provider.Find("relay")
		p.Contexts = map[string]int{"gpt-6.1-terra": 400000}
		if err := provider.Save(*p); err != nil {
			t.Fatal(err)
		}
		if err := provider.SetModelEfforts("relay/gpt-6.1-terra", []string{"low", "high"}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { provider.SetModelEfforts("relay/gpt-6.1-terra", nil) })
		e := codexEntry(t, "relay/gpt-6.1-terra")
		verbatim(t, e, "context_window", "supported_reasoning_levels", "default_reasoning_level")
		if w, _ := e["context_window"].(float64); w != 400000 {
			t.Errorf("context_window %v, want the user's 400000", e["context_window"])
		}
		levels, _ := json.Marshal(e["supported_reasoning_levels"])
		if want := `[{"description":"Fast responses with lighter reasoning","effort":"low"},{"description":"Greater reasoning depth for complex problems","effort":"high"}]`; string(levels) != want {
			t.Errorf("levels %s, want %s", levels, want)
		}
		if d, _ := e["default_reasoning_level"].(string); d != "low" && d != "high" {
			t.Errorf("default_reasoning_level %q, not one of the user's levels", d)
		}
	})
}

// A relay whose plain list is already in Codex's shape is asked once.
func TestCodexListedEntryInPlainList(t *testing.T) {
	fresh(t)
	relay, base := newRelay(t)
	relay.plain.Store(`{"models":[` + terraEntry + `]}`)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Models: []string{"gpt-6.1-terra"}, Responses: base}); err != nil {
		t.Fatal(err)
	}
	fetchRelay(t)
	if n := relay.asked.Load(); n != 0 {
		t.Errorf("asked again with client_version %d times, want none", n)
	}
	e := codexEntry(t, "relay/gpt-6.1-terra")
	if e["base_instructions"] != "You are Codex, based on GPT-6.1 Terra. You are running as a coding agent in the Codex CLI on a user's computer." {
		t.Errorf("instructions %.80v, want the relay's", e["base_instructions"])
	}
	if w, _ := e["context_window"].(float64); w != 272000 {
		t.Errorf("context_window %v, want the relay's 272000", e["context_window"])
	}
}
