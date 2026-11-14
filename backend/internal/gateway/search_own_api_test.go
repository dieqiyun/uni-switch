package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A request that offers web search, at a provider that doesn't search by
// itself but serves the client's own API, is translated on that API, not
// on Chat (#997, wlj521: Codex on Xiaomi MiMo's pay as you go, whose
// /v1/responses is served, showed "Responses → Chat" — Codex offers its
// web_search on every turn). The provider is the preset's, the three URLs
// of its pay as you go region; the search tool, which the provider would
// turn away or ignore, isn't sent to it.
func TestSearchOfferedStaysOnTheClientsAPI(t *testing.T) {
	srv, seen := relayOfEvery(t)
	fresh(t)
	if err := provider.Save(provider.Provider{ID: "xiaomi", Name: "Xiaomi MiMo", Key: "k", Preset: "xiaomi",
		Chat: srv.URL + "/v1", Responses: srv.URL + "/v1", Anthropic: srv.URL,
		Models: []string{"mimo-v2.6-flash"}}); err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct{ name, path, body, want string }{
		{"Codex", "/v1/responses", `{"model":"xiaomi/mimo-v2.6-flash","stream":true,"store":false,"reasoning":{"effort":"medium"},"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}],
			"tools":[{"type":"function","name":"shell","description":"run","parameters":{"type":"object","properties":{}}},{"type":"web_search","external_web_access":false}]}`, "/v1/responses"},
		{"Claude Code", "/v1/messages", `{"model":"xiaomi/mimo-v2.6-flash","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hi"}],
			"tools":[{"name":"Read","description":"read","input_schema":{"type":"object"}},{"type":"web_search_20250305","name":"web_search","max_uses":8}]}`, "/v1/messages"},
		{"Chat", "/v1/chat/completions", `{"model":"xiaomi/mimo-v2.6-flash","stream":true,"messages":[{"role":"user","content":"hi"}],"web_search_options":{}}`, "/v1/chat/completions"},
	} {
		seen()
		if code, b := post(t, r.path, r.body); code != 200 {
			t.Fatalf("%s: %d %s", r.name, code, b)
		}
		paths, bodies := seen()
		if len(paths) != 1 || paths[0] != r.want {
			t.Errorf("%s with web search offered went to %v, want %s", r.name, paths, r.want)
			continue
		}
		if strings.Contains(bodies[0], "web_search") {
			t.Errorf("%s: the search tool reached a provider that doesn't search: %s", r.name, bodies[0])
		}
	}
}

// Codex's web_search at a provider serving Responses that doesn't search:
// the model, asked on Responses, is given uni-switch's web_search function,
// the search is done by a provider that can, and Codex gets the answer
// (#997).
func TestCodexSearchAnsweredOnResponses(t *testing.T) {
	var mu sync.Mutex
	var searched int
	var paths []string
	search := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			io.WriteString(w, `{"data":[{"id":"claude-haiku-4-5"}]}`)
		case "/v1/messages":
			mu.Lock()
			searched++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"id":"s","type":"message","role":"assistant","model":"claude-haiku-4-5","content":[
				{"type":"server_tool_use","id":"srv","name":"web_search","input":{"query":"go"}},
				{"type":"web_search_tool_result","tool_use_id":"srv","content":[{"type":"web_search_result","title":"Go downloads","url":"https://go.dev/dl/"}]},
				{"type":"text","text":"The latest Go is 1.27.1."}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":5}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer search.Close()
	var asked []string
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		paths = append(paths, r.URL.Path)
		asked = append(asked, string(b))
		n := len(asked)
		mu.Unlock()
		if r.URL.Path != "/v1/responses" {
			http.Error(w, `{"error":{"message":"asked on `+r.URL.Path+`"}}`, 400)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			io.WriteString(w, sse(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"web_search","arguments":""}}`,
				`data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"{\"query\":\"latest go\"}"}`,
				`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"web_search","arguments":"{\"query\":\"latest go\"}"}}`,
				`data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[{"type":"function_call","id":"fc_1","call_id":"call_1","name":"web_search","arguments":"{\"query\":\"latest go\"}"}],"usage":{"input_tokens":30,"output_tokens":7}}}`))
			return
		}
		io.WriteString(w, sse(`data: {"type":"response.output_text.delta","item_id":"m","output_index":0,"content_index":0,"delta":"Go 1.27.1 is out."}`,
			`data: {"type":"response.completed","response":{"id":"r2","status":"completed","output":[{"type":"message","id":"m","role":"assistant","content":[{"type":"output_text","text":"Go 1.27.1 is out."}]}],"usage":{"input_tokens":10,"output_tokens":5}}}`))
	}))
	defer model.Close()

	fresh(t)
	hosts := searchHosts[provider.Anthropic]
	searchHosts[provider.Anthropic] = append(hosts, provider.HostOf(search.URL))
	defer func() { searchHosts[provider.Anthropic] = hosts }()
	for _, p := range []provider.Provider{
		{ID: "srch", Name: "Search", Key: "k", Anthropic: search.URL},
		{ID: "xiaomi", Name: "Xiaomi MiMo", Key: "k", Preset: "xiaomi", Chat: model.URL + "/v1", Responses: model.URL + "/v1", Models: []string{"mimo-v2.6-flash"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	p, _ := provider.Find("srch")
	if _, err := p.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	code, out := post(t, "/v1/responses", `{"model":"xiaomi/mimo-v2.6-flash","stream":true,"instructions":"You are a coding assistant.","input":[`+
		codexItem("developer", "Plan: do not make changes.")+`,`+
		codexItem("user", "Plan a version check.")+`,`+
		codexItem("assistant", "Check the latest Go release.")+`,`+
		codexItem("developer", "Default: previous Plan instructions no longer apply.")+`,{"type":"message","role":"user","content":[{"type":"input_text","text":"What is the latest Go?"}]}],
		"tools":[{"type":"function","name":"shell","description":"run","parameters":{"type":"object","properties":{}}},{"type":"web_search","external_web_access":false}]}`)
	if code != 200 {
		t.Fatalf("%d %s", code, out)
	}
	if !strings.Contains(out, "Go 1.27.1 is out.") {
		t.Fatalf("no answer: %s", out)
	}
	if searched != 1 {
		t.Fatalf("searched %d times", searched)
	}
	if len(paths) != 2 || paths[0] != "/v1/responses" || paths[1] != "/v1/responses" {
		t.Fatalf("model asked on %v, want Responses twice", paths)
	}
	if !strings.Contains(asked[0], `"web_search"`) || !strings.Contains(asked[1], "1.27.1") {
		t.Fatalf("search tool or result not given to the model:\n%s\n%s", asked[0], asked[1])
	}
	for i, body := range asked {
		var sent struct {
			Instructions string `json:"instructions"`
			Input        []struct {
				Role    string
				Content json.RawMessage
			} `json:"input"`
		}
		if err := json.Unmarshal([]byte(body), &sent); err != nil {
			t.Fatal(err)
		}
		if sent.Instructions != "You are a coding assistant." || len(sent.Input) < 5 ||
			sent.Input[0].Role != "developer" || sent.Input[3].Role != "developer" ||
			text(responsesParts(sent.Input[3].Content)) != "Default: previous Plan instructions no longer apply." {
			t.Fatalf("search round %d changed instruction authority: %s", i+1, body)
		}
	}
}
