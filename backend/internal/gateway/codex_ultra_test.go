package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/codexcat"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// copilotUp is a Copilot account whose API serves gpt-6.1-sol and gpt-6-luna
// on /responses, each up to max, as Copilot's /models lists them (#656). It
// keeps the bodies it was sent; a tool whose arguments it is asked to seal
// ("encrypted": true) it answers sealed, as OpenAI's server does.
type copilotUp struct {
	mu     sync.Mutex
	bodies []map[string]any
	reply  string // a stream to answer with, in place of the made one
}

func (c *copilotUp) last(t *testing.T) map[string]any {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.bodies) == 0 {
		t.Fatal("Copilot was asked nothing")
	}
	return c.bodies[len(c.bodies)-1]
}

func copilotSol(t *testing.T) *copilotUp {
	t.Helper()
	fresh(t)
	cfg := os.Getenv("XDG_CONFIG_HOME")
	os.MkdirAll(filepath.Join(cfg, "github-copilot"), 0o755)
	os.WriteFile(filepath.Join(cfg, "github-copilot", "apps.json"), mustJSON(map[string]any{
		"github.com:Iv1.x": map[string]any{"user": "wlll5", "oauth_token": "gho_656_" + t.Name()},
	}), 0o600)
	c := &copilotUp{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			levels := `"reasoning_effort":["none","low","medium","high","xhigh","max"]`
			io.WriteString(w, `{"data":[
			  {"id":"gpt-6.1-sol","name":"GPT-6.1 Sol","model_picker_enabled":true,"policy":{"state":"enabled"},"supported_endpoints":["/responses"],"capabilities":{"type":"chat","supports":{`+levels+`}}},
			  {"id":"gpt-6-luna","name":"GPT-6 Luna","model_picker_enabled":true,"policy":{"state":"enabled"},"supported_endpoints":["/responses"],"capabilities":{"type":"chat","supports":{`+levels+`}}}]}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		json.Unmarshal(b, &body)
		c.mu.Lock()
		c.bodies = append(c.bodies, body)
		reply := c.reply
		c.mu.Unlock()
		if reply != "" {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, reply)
			return
		}
		message := "Find the release date of Codex 0.159 and report it."
		if strings.Contains(string(b), `"encrypted"`) {
			message = "gAAAAAsealed=="
		}
		w.Header().Set("Content-Type", "text/event-stream")
		item := `{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"done"}]}`
		if strings.Contains(string(b), "Spawn") {
			args, _ := json.Marshal(`{"task_name":"research","message":` + string(mustJSON(message)) + `}`)
			item = `{"type":"function_call","id":"fc_1","call_id":"call_1","name":"collaboration__spawn_agent","arguments":` + string(args) + `,"status":"completed"}`
		}
		added := strings.Replace(strings.Replace(item, `"text":"done"`, `"text":""`, 1), `"arguments":`, `"arguments":"","was":`, 1)
		io.WriteString(w, sse(
			`data: {"type":"response.created","response":{"id":"r1","model":"gpt-6.1-sol"}}`,
			`data: {"type":"response.output_item.added","output_index":0,"item":`+added+`}`,
			`data: {"type":"response.output_item.done","output_index":0,"item":`+item+`}`,
			`data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":5}}}`,
		))
	}))
	t.Cleanup(api.Close)
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			json.NewEncoder(w).Encode(map[string]any{"token": "sess", "expires_at": time.Now().Add(time.Hour).Unix(), "endpoints": map[string]string{"api": api.URL}})
			return
		}
		io.WriteString(w, `{"copilot_plan":"individual","access_type_sku":"monthly_subscriber_quota"}`)
	}))
	t.Cleanup(gh.Close)
	oldTok, oldUser := provider.CopilotTokenURL, provider.CopilotUserURL
	provider.CopilotTokenURL, provider.CopilotUserURL = gh.URL+"/token", gh.URL+"/user"
	t.Cleanup(func() { provider.CopilotTokenURL, provider.CopilotUserURL = oldTok, oldUser })
	p, err := provider.Find("copilot")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	return c
}

// Codex's Ultra on Copilot's GPT-6.1 Sol (#656): uni-switch's /v1/models and
// the list it hands Codex offer it there, not on GPT-6 Luna, which OpenAI
// offers up to max alone; Codex is told multi-agent V2 for it, where Ultra
// hands work to its agents. An "ultra" that reaches the gateway goes to
// Copilot as max, relayed as it came or made again (Codex's web_search on).
func TestCopilotSolOffersUltra(t *testing.T) {
	c := copilotSol(t)
	s := New()

	levels := map[string][]string{}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	var list struct {
		Data []struct {
			ID     string `json:"id"`
			Levels []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &list)
	for _, m := range list.Data {
		for _, l := range m.Levels {
			levels[m.ID] = append(levels[m.ID], l.Effort)
		}
	}
	if !slices.Contains(levels["copilot/gpt-6.1-sol"], "ultra") || !slices.Contains(levels["copilot/gpt-6.1-sol"], "max") {
		t.Fatalf("/v1/models copilot/gpt-6.1-sol: %v\n%s", levels["copilot/gpt-6.1-sol"], rec.Body.String())
	}
	if l := levels["copilot/gpt-6-luna"]; !slices.Contains(l, "max") || slices.Contains(l, "ultra") {
		t.Fatalf("/v1/models copilot/gpt-6-luna: %v", l)
	}

	// the list Codex is handed
	var cat struct {
		Models []map[string]any `json:"models"`
	}
	json.Unmarshal(codexcat.Catalog(provider.CodexListed()), &cat)
	found := 0
	for _, m := range cat.Models {
		var efforts []string
		for _, l := range m["supported_reasoning_levels"].([]any) {
			efforts = append(efforts, l.(map[string]any)["effort"].(string))
		}
		switch m["slug"] {
		case "copilot/gpt-6.1-sol":
			found++
			if !slices.Contains(efforts, "ultra") || m["multi_agent_version"] != "v2" {
				t.Fatalf("Codex's copilot/gpt-6.1-sol: %v %v", efforts, m["multi_agent_version"])
			}
		case "copilot/gpt-6-luna":
			found++
			if slices.Contains(efforts, "ultra") || m["multi_agent_version"] != nil {
				t.Fatalf("Codex's copilot/gpt-6-luna: %v %v", efforts, m["multi_agent_version"])
			}
		}
	}
	if found != 2 {
		t.Fatalf("Codex's list: %v", cat.Models)
	}

	for _, search := range []bool{false, true} {
		tools := ``
		if search {
			tools = `,"tools":[{"type":"web_search"}]`
		}
		code, out := postTo(t, s, "/v1/responses", `{"model":"copilot/gpt-6.1-sol","stream":true,"reasoning":{"effort":"ultra"},"input":"hi"`+tools+`}`)
		if code != 200 {
			t.Fatalf("search %v: %d %s", search, code, out)
		}
		r, _ := c.last(t)["reasoning"].(map[string]any)
		if r["effort"] != "max" {
			t.Fatalf("search %v: Copilot was sent effort %v", search, r["effort"])
		}
	}
}

// Ultra's subagents with Codex's web_search kept (#656): a lead whose
// spawn_agent marks its message "encrypted" is made again for Copilot
// rather than relayed, as Copilot can't open what it seals, and on that
// path its spawn_agent comes back to Codex unsealed, with the task as
// written (Copilot not asked to seal it). The subagent's request, which
// marks nothing, is relayed with the search (Copilot runs web_search
// itself), the task as MultiAgentV2's agent_message as Codex sent it.
func TestCopilotUltraSubagentWithWebSearch(t *testing.T) {
	c := copilotSol(t)
	s := New()
	const task = "Find the release date of Codex 0.159 and report it."
	tools := `"tools":[{"type":"namespace","name":"collaboration","description":"Sub-agents.","tools":[
		{"type":"function","name":"spawn_agent","strict":false,"description":"Spawns an agent.",
		 "parameters":{"type":"object","properties":{"task_name":{"type":"string"},"message":{"type":"string","encrypted":true}},
		 "required":["task_name","message"],"additionalProperties":false}}]},
		{"type":"web_search"}]`

	code, out := postTo(t, s, "/v1/responses", `{"model":"copilot/gpt-6.1-sol","stream":true,"reasoning":{"effort":"max"},
		"input":[{"role":"user","content":"Spawn a subagent to research this."}],`+tools+`,"tool_choice":"auto"}`)
	if code != 200 {
		t.Fatalf("lead: %d %s", code, out)
	}
	// made again for Copilot, not relayed: its tools flat, the search
	// uni-switch's
	if lead, _ := json.Marshal(c.last(t)["tools"]); !strings.Contains(string(lead), `"collaboration__spawn_agent"`) || strings.Contains(string(lead), `"encrypted"`) {
		t.Fatalf("lead's tools to Copilot: %s", lead)
	}
	var call map[string]any
	for _, line := range strings.Split(out, "\n") {
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		var ev struct {
			Type string         `json:"type"`
			Item map[string]any `json:"item"`
		}
		if json.Unmarshal([]byte(data), &ev) == nil && ev.Type == "response.output_item.done" && ev.Item["type"] == "function_call" {
			call = ev.Item
		}
	}
	if call == nil || call["name"] != "spawn_agent" || call["namespace"] != "collaboration" {
		t.Fatalf("lead's call: %v\n%s", call, out)
	}
	// Codex reads the message as plain text only with an empty list here
	// (codex-rs ToolCall::direct_source)
	if sealed, ok := call["encrypted_function_args"].([]any); !ok || len(sealed) != 0 {
		t.Fatalf("lead's call is read as sealed: %v", call)
	}
	var args struct {
		Message string `json:"message"`
	}
	json.Unmarshal([]byte(call["arguments"].(string)), &args)
	if args.Message != task {
		t.Fatalf("subagent's task: %q", args.Message)
	}

	// the subagent, on Copilot's GPT-6 Luna, with the search still offered
	code, out = postTo(t, s, "/v1/responses", `{"model":"copilot/gpt-6-luna","stream":true,
		"input":[{"type":"agent_message","author":"/root","recipient":"/root/research","content":[{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\n`+task+`"}]}],
		"tools":[{"type":"web_search"}]}`)
	if code != 200 {
		t.Fatalf("subagent: %d %s", code, out)
	}
	sub := c.last(t)
	sent, _ := json.Marshal(sub["input"])
	if !strings.Contains(string(sent), task) || !strings.Contains(string(sent), `"agent_message"`) || strings.Contains(string(sent), "encrypted_content") {
		t.Fatalf("subagent's input to Copilot: %s", sent)
	}
	if offered, _ := json.Marshal(sub["tools"]); string(offered) != `[{"type":"web_search"}]` {
		t.Fatalf("subagent's tools to Copilot: %s", offered)
	}
}

// A Codex turn on Copilot offering web_search is relayed as Codex sent it,
// as Copilot runs the search itself (echo_ts on Discord: translated for
// uni-switch's search, every turn lost the relay); one whose tools mark an
// argument "encrypted" is made again unmarked, with the search or without,
// since Copilot seals that argument and can't open it on the next turn.
func TestCopilotRelayedUnlessAskedToSeal(t *testing.T) {
	c := copilotSol(t)
	s := New()
	spawn := `{"type":"namespace","name":"collaboration","description":"Sub-agents.","tools":[
		{"type":"function","name":"spawn_agent","strict":false,"description":"Spawns an agent.",
		 "parameters":{"type":"object","properties":{"message":{"type":"string","encrypted":true}},"required":["message"]}}]}`
	exec := `{"type":"function","name":"exec_command","description":"Runs a command, \"encrypted\" or not.","strict":false,
		 "parameters":{"type":"object","properties":{"cmd":{"type":"string"},"encrypted":{"type":"boolean"}},"required":["cmd"]}}`
	for _, tc := range []struct {
		name, tools string
		relayed     bool
	}{
		{"search", exec + `,{"type":"web_search"}`, true},
		{"search and a sealed argument", spawn + `,` + exec + `,{"type":"web_search"}`, false},
		{"a sealed argument alone", spawn, false},
	} {
		code, out := postTo(t, s, "/v1/responses", `{"model":"copilot/gpt-6-luna","stream":true,"input":[{"role":"user","content":"hi"}],"probe":"as sent","tools":[`+tc.tools+`]}`)
		if code != 200 {
			t.Fatalf("%s: %d %s", tc.name, code, out)
		}
		// a field uni-switch doesn't know goes on only as Codex sent it
		body := c.last(t)
		sent, _ := json.Marshal(body["tools"])
		relayed := body["probe"] == "as sent"
		if relayed != tc.relayed {
			t.Errorf("%s: relayed %v, tools to Copilot %s", tc.name, relayed, sent)
		}
		if !tc.relayed && strings.Contains(string(sent), `"encrypted":true`) {
			t.Errorf("%s: Copilot was asked to seal: %s", tc.name, sent)
		}
	}
}

// Ultra is Codex's, no API's: a model whose list says "ultra" (ChatGPT's
// lists it for Codex's picker) is sent max all the same.
func TestUltraIsSentAsMax(t *testing.T) {
	for _, c := range []struct {
		levels []string
		want   string
	}{
		{[]string{"low", "medium", "high", "xhigh", "max", "ultra"}, "max"},
		{[]string{"low", "medium", "high", "xhigh", "max"}, "max"},
		{[]string{"low", "medium", "high", "ultra"}, "high"},
		{nil, "max"},
	} {
		if got := fitEffort("ultra", c.levels); got != c.want {
			t.Errorf("fitEffort(ultra, %v) = %q, want %q", c.levels, got, c.want)
		}
	}
}
