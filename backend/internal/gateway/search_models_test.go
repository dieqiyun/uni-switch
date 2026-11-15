package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// Command Code's Provider API searches the web by itself for some of its
// models only (#1590, M3chD09: 12 of 87, its GPT, Grok and Muse models on
// Responses, its Claude ones on Messages). Those are sent the agent's own
// web search tool; the rest keep uni-switch's searcher, and so does every
// model when the provider is sent Command Code's zero-data-retention
// header, under which it refuses every built-in tool.
func TestCommandCodeSearchesForSomeModels(t *testing.T) {
	srv, seen := relayOfEvery(t)
	fresh(t)
	save := func(h map[string]string) {
		t.Helper()
		if err := provider.Save(provider.Provider{ID: "commandcode", Name: "Command Code", Key: "k", Preset: "commandcode",
			Chat: srv.URL + "/v1", Responses: srv.URL + "/v1", Anthropic: srv.URL, Headers: h,
			Models: []string{"gpt-6-luna", "xai/grok-4.7", "deepseek/deepseek-v4-pro", "claude-sonnet-5-5", "claude-opus-5-5"}}); err != nil {
			t.Fatal(err)
		}
	}
	codex := func(model string) string {
		return `{"model":"commandcode/` + model + `","stream":true,"store":false,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"news?"}]}],
			"tools":[{"type":"function","name":"shell","description":"run","parameters":{"type":"object","properties":{}}},{"type":"web_search","external_web_access":true}]}`
	}
	claude := func(model string) string {
		return `{"model":"commandcode/` + model + `","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"news?"}],
			"tools":[{"name":"Read","description":"read","input_schema":{"type":"object"}},{"type":"web_search_20250305","name":"web_search","max_uses":8}]}`
	}
	cases := []struct {
		name, path, body, want string
		native                 bool
	}{
		{"gpt-6-luna", "/v1/responses", codex("gpt-6-luna"), "/v1/responses", true},
		{"xai/grok-4.7", "/v1/responses", codex("xai/grok-4.7"), "/v1/responses", true},
		{"deepseek-v4-pro", "/v1/responses", codex("deepseek/deepseek-v4-pro"), "/v1/responses", false},
		{"claude-sonnet-5-5", "/v1/messages", claude("claude-sonnet-5-5"), "/v1/messages", true},
		{"claude-opus-5-5", "/v1/messages", claude("claude-opus-5-5"), "/v1/messages", false},
	}
	run := func(zdr bool) {
		for _, r := range cases {
			seen()
			if code, b := post(t, r.path, r.body); code != 200 {
				t.Fatalf("%s: %d %s", r.name, code, b)
			}
			paths, bodies := seen()
			if len(paths) != 1 || paths[0] != r.want {
				t.Errorf("%s (zdr %v) went to %v, want %s", r.name, zdr, paths, r.want)
				continue
			}
			got := strings.Contains(bodies[0], `"type":"web_search`)
			if want := r.native && !zdr; got != want {
				t.Errorf("%s (zdr %v): the vendor's own search tool sent = %v, want %v: %s", r.name, zdr, got, want, bodies[0])
			}
		}
	}
	save(nil)
	run(false)
	save(map[string]string{"x-cmd-zdr": "1"})
	run(true)
}

// A model that searches by itself is offered in Settings' searcher picker
// and picked by uni-switch by the same rule that sends it the vendor's search
// tool (modelSearches), not only a provider whose every model searches:
// Command Code's searching models searched for themselves, but were left
// out of the picker, so a model that can't search was searched for by
// another provider or by none (01huadalang on Discord: the picker named
// Antigravity alone). Only its models that search are offered, and none
// under its zero-data-retention header.
func TestCommandCodeSearchesForOtherModels(t *testing.T) {
	searchSandbox(t)
	var mu sync.Mutex
	var asked []string
	cc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "/models") {
			io.WriteString(w, `{"data":[{"id":"claude-opus-5-5"},{"id":"deepseek/deepseek-v4-pro"},{"id":"gpt-6-luna"}]}`)
			return
		}
		var q struct {
			Model string `json:"model"`
		}
		json.Unmarshal(b, &q)
		mu.Lock()
		asked = append(asked, r.URL.Path+" "+q.Model)
		mu.Unlock()
		if r.URL.Path != "/v1/responses" || q.Model != "gpt-6-luna" || !strings.Contains(string(b), `"web_search"`) {
			http.Error(w, `{"error":{"message":"no search here"}}`, 400)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(`data: {"type":"response.created","response":{"id":"r1","model":"gpt-6-luna"}}`,
			`data: {"type":"response.output_item.done","item":{"type":"web_search_call","status":"completed","action":{"type":"search","query":"今天上海天气","sources":[{"type":"url","url":"https://www.weather.com.cn/weather/101020100.shtml"}]}}}`,
			`data: {"type":"response.output_text.delta","delta":"上海今天小雨，22/17℃。"}`,
			`data: {"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":4000,"output_tokens":20}}}`))
	}))
	t.Cleanup(cc.Close)
	glm, result := newBlindModel(t)
	commandCode := func(h map[string]string) provider.Provider {
		return provider.Provider{ID: "commandcode", Name: "Command Code", Key: "k", Preset: "commandcode", Headers: h,
			Chat: cc.URL + "/v1", Responses: cc.URL + "/v1", Anthropic: cc.URL,
			Models: []string{"claude-opus-5-5", "deepseek/deepseek-v4-pro", "gpt-6-luna"}}
	}
	save(t, commandCode(nil), provider.Provider{ID: "glm", Name: "GLM", Key: "k", Chat: glm.URL, Models: []string{"glm-flash"}})

	var offered []string
	for _, c := range Searchers() {
		for _, m := range c.Models {
			offered = append(offered, c.Provider.ID+"/"+m.ID)
		}
		if c.Provider.ID == "commandcode" && (c.Small != "gpt-6-luna" || c.ManualOnly) {
			t.Fatalf("Command Code searches with %q, manual only %v", c.Small, c.ManualOnly)
		}
	}
	if !slices.Equal(offered, []string{"commandcode/gpt-6-luna"}) {
		t.Fatalf("the picker offers %v, want Command Code's gpt-6-luna alone", offered)
	}
	if p, m, ok := searcher(); !ok || p.ID != "commandcode" || m != "gpt-6-luna" {
		t.Fatalf("searcher = %s %s %v", p.ID, m, ok)
	}
	st := settings.Load()
	st.Searcher = "commandcode/claude-opus-5-5" // a model of it that doesn't search
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	if p, m, ok := searcher(); !ok || p.ID != "commandcode" || m != "gpt-6-luna" {
		t.Fatalf("named a model that doesn't search, searcher = %s %s %v", p.ID, m, ok)
	}
	st.Searcher = ""
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}

	// GLM, which can't search, is searched for by gpt-6-luna on Responses
	blocks := searchAsClaudeCode(t)
	mu.Lock()
	got := slices.Clone(asked)
	mu.Unlock()
	if !slices.Equal(got, []string{"/v1/responses gpt-6-luna"}) {
		t.Fatalf("Command Code asked %q", got)
	}
	if len(blocks) != 1 {
		t.Fatalf("blocks %v", blocks)
	}
	if found, _ := blocks[0]["content"].([]any); len(found) != 1 || found[0].(map[string]any)["url"] != "https://www.weather.com.cn/weather/101020100.shtml" {
		t.Fatalf("found %v", blocks[0])
	}
	if r := result(); !strings.Contains(r, "22/17℃") {
		t.Fatalf("the model was told %q", r)
	}

	// under x-cmd-zdr none of its models searches, so none is offered
	save(t, commandCode(map[string]string{"x-cmd-zdr": "1"}))
	for _, c := range Searchers() {
		if c.Provider.ID == "commandcode" {
			t.Errorf("Command Code under x-cmd-zdr offered with %v", c.Models)
		}
	}
	if p, m, ok := searcher(); ok {
		t.Errorf("under x-cmd-zdr, searcher = %s %s", p.ID, m)
	}
}
