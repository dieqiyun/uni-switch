package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// zenServer is a gateway whose provider zen is OpenCode Zen (chat
// completions), answering each request with the chat SSE stream reply, and
// other an unrelated one answering the same; it records the bodies sent.
func zenServer(t *testing.T, reply string) (*Server, map[string][]map[string]any) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, p := range []provider.Provider{
		{ID: "zen", Name: "Zen", Key: "public", Chat: "https://opencode.ai/zen/v1", Models: []string{"mimo-v2.6-flash-free", "kimi-k2.6", "big-pickle", "nemo-zero"}},
		{ID: "other", Name: "Other", Key: "k", Chat: "https://other.test/v1", Models: []string{"mimo-v2.6-flash-free"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string][]map[string]any{}
	s := New()
	s.client = &http.Client{Transport: countTransport(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		got[r.URL.Host] = append(got[r.URL.Host], m)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(reply))}, nil
	})}
	return s, got
}

// chatSSE is a chat completions stream: text, then a call of tool (if any)
// with args, then finish.
func chatSSE(text, tool, args string) string {
	var b strings.Builder
	chunk := func(delta, finish string) {
		f := "null"
		if finish != "" {
			f = `"` + finish + `"`
		}
		b.WriteString(`data: {"id":"c","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":` + delta + `,"finish_reason":` + f + `}]}` + "\n\n")
	}
	chunk(`{"role":"assistant","content":""}`, "")
	if text != "" {
		chunk(`{"content":`+jsonStr(text)+`}`, "")
	}
	finish := "stop"
	if tool != "" {
		chunk(`{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"`+tool+`","arguments":""}}]}`, "")
		chunk(`{"tool_calls":[{"index":0,"function":{"arguments":`+jsonStr(args)+`}}]}`, "")
		finish = "tool_calls"
	}
	chunk(`{}`, finish)
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

func jsonStr(s string) string { b, _ := json.Marshal(s); return string(b) }

func toolNames(m map[string]any) []string {
	var out []string
	tools, _ := m["tools"].([]any)
	for _, t := range tools {
		tm, _ := t.(map[string]any)
		if f, ok := tm["function"].(map[string]any); ok {
			tm = f
		}
		n, _ := tm["name"].(string)
		out = append(out, n)
	}
	return out
}

func zenSend(t *testing.T, s *Server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("anthropic-version", "2023-06-01")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
	}
	return rec
}

const claudeTools = `"tools":[{"name":"Bash","description":"run","input_schema":{"type":"object","properties":{"command":{"type":"string"}}}},` +
	`{"name":"Read","description":"read","input_schema":{"type":"object","properties":{"file_path":{"type":"string"}}}},` +
	`{"name":"Grep","description":"grep","input_schema":{"type":"object"}}]`

// Claude Code's Read and Grep go to Zen's free models as OpenCode's read
// and grep, beside stubs of the core tools it lacks (its Bash is not
// shell), in a streamed request, and the model's call of read comes back
// to Claude Code as Read, in the stream it asked for and in a reply it
// didn't stream.
func TestZenFreeClaudeCodeTools(t *testing.T) {
	s, got := zenServer(t, chatSSE("Reading it.", "read", `{"path":"a"}`))
	history := `"messages":[{"role":"user","content":"list"},` +
		`{"role":"assistant","content":[{"type":"tool_use","id":"t0","name":"Read","input":{"file_path":"a"}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"t0","content":"ok"}]}]`
	rec := zenSend(t, s, "/v1/messages", `{"model":"zen/mimo-v2.6-flash-free","max_tokens":100,"stream":true,`+claudeTools+`,"tool_choice":{"type":"tool","name":"Read"},`+history+`}`)
	sent := got["opencode.ai"][0]
	if sent["stream"] != true {
		t.Fatalf("not streamed: %v", sent)
	}
	if n := toolNames(sent); strings.Join(n, ",") != "Bash,read,grep,edit,glob,shell" {
		t.Fatalf("tools %v", n)
	}
	tc, _ := json.Marshal(sent["tool_choice"])
	if !strings.Contains(string(tc), `"read"`) {
		t.Fatalf("tool_choice %s", tc)
	}
	hist, _ := json.Marshal(sent["messages"])
	if !strings.Contains(string(hist), `"name":"read"`) || strings.Contains(string(hist), `"Read"`) {
		t.Fatalf("history %s", hist)
	}
	out := rec.Body.String()
	if !strings.Contains(out, `"name":"Read"`) || strings.Contains(out, `"name":"read"`) || !strings.Contains(out, "tool_use") {
		t.Fatalf("stream %s", out)
	}

	// not streamed: gathered, the name mapped back all the same
	rec = zenSend(t, s, "/v1/messages", `{"model":"zen/mimo-v2.6-flash-free","max_tokens":100,`+claudeTools+`,"messages":[{"role":"user","content":"list"}]}`)
	if got["opencode.ai"][1]["stream"] != true {
		t.Fatal("upstream not streamed")
	}
	var msg struct {
		Content    []struct{ Type, Name, Text string }
		StopReason string `json:"stop_reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &msg); err != nil {
		t.Fatalf("%v: %s", err, rec.Body)
	}
	var names []string
	for _, c := range msg.Content {
		if c.Type == "tool_use" {
			names = append(names, c.Name)
		}
	}
	if strings.Join(names, ",") != "Read" || msg.StopReason != "tool_use" {
		t.Fatalf("reply %s", rec.Body)
	}
}

// Codex's shell is OpenCode's shell by name: Zen is offered stubs of the
// other core tools beside it, and a call of a stub never reaches Codex; the turn ends with what the
// model said.
func TestZenFreeCodexStubs(t *testing.T) {
	s, got := zenServer(t, chatSSE("Let me look.", "read", `{"path":"x"}`))
	rec := zenSend(t, s, "/v1/responses", `{"model":"zen/mimo-v2.6-flash-free","stream":true,"input":"look",`+
		`"tools":[{"type":"function","name":"shell","parameters":{"type":"object","properties":{"command":{"type":"array"}}}}],"tool_choice":"auto"}`)
	sent := got["opencode.ai"][0]
	if n := toolNames(sent); strings.Join(n, ",") != "shell,edit,glob,grep,read" {
		t.Fatalf("tools %v", n)
	}
	if sent["tool_choice"] != "auto" {
		t.Fatalf("tool_choice %v", sent["tool_choice"])
	}
	out := rec.Body.String()
	if strings.Contains(out, "function_call") || strings.Contains(out, `"read"`) || !strings.Contains(out, "Let me look.") || !strings.Contains(out, "response.completed") {
		t.Fatalf("stream %s", out)
	}

	// a stub called with nothing said: a short note, a plain stop
	s, _ = zenServer(t, chatSSE("", "shell", `{}`))
	rec = zenSend(t, s, "/v1/chat/completions", `{"model":"zen/mimo-v2.6-flash-free","messages":[{"role":"user","content":"hi"}]}`)
	var res struct {
		Choices []struct {
			Message struct {
				Content   string
				ToolCalls []any `json:"tool_calls"`
			}
			FinishReason string `json:"finish_reason"`
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || len(res.Choices) != 1 {
		t.Fatalf("%v: %s", err, rec.Body)
	}
	c := res.Choices[0]
	if c.Message.Content != zenStubNote || len(c.Message.ToolCalls) != 0 || c.FinishReason != "stop" {
		t.Fatalf("reply %s", rec.Body)
	}
}

// Only Zen's free models are reshaped: Zen's paid ones and another
// provider's -free model go as they came, relayed.
func TestZenFreeOnlyFreeModels(t *testing.T) {
	s, got := zenServer(t, chatSSE("hi", "", ""))
	body := `{"model":"%s","stream":true,"messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"Bash","parameters":{"type":"object"}}}]}`
	for _, m := range []string{"zen/kimi-k2.6", "other/mimo-v2.6-flash-free"} {
		zenSend(t, s, "/v1/chat/completions", strings.Replace(body, "%s", m, 1))
	}
	for _, host := range []string{"opencode.ai", "other.test"} {
		if n := toolNames(got[host][0]); strings.Join(n, ",") != "Bash" {
			t.Fatalf("%s: tools %v", host, n)
		}
	}
}

// Zen's free models whose ids don't end in -free are asked as OpenCode
// asks too (361 on Discord: big-pickle answered 403 FreeTierError): one
// known without the catalog, and one models.dev's opencode prices at
// nothing; one it prices is not.
func TestZenFreeZeroCostModels(t *testing.T) {
	s, got := zenServer(t, chatSSE("hi", "", ""))
	if err := os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.CachePath(), []byte(`{"opencode":{"models":{
		"nemo-zero":{"id":"nemo-zero","cost":{"input":0,"output":0}},
		"kimi-k2.6":{"id":"kimi-k2.6","cost":{"input":0.6,"output":2.5}}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	body := `{"model":"%s","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"Bash","parameters":{"type":"object"}}}]}`
	for i, m := range []string{"zen/big-pickle", "zen/nemo-zero", "zen/kimi-k2.6"} {
		zenSend(t, s, "/v1/chat/completions", strings.Replace(body, "%s", m, 1))
		sent := got["opencode.ai"][i]
		free := i < 2
		if n := strings.Join(toolNames(sent), ","); (n == "Bash,edit,glob,grep,read,shell") != free {
			t.Errorf("%s: tools %s", m, n)
		}
		if (sent["stream"] == true) != free {
			t.Errorf("%s: stream %v", m, sent["stream"])
		}
	}
}

// A request that searches is asked in rounds (searchReply): each round's
// reply comes back with the client's names and without a stub's call.
func TestZenFreeSearchRounds(t *testing.T) {
	z := zenFreeTools(&Request{Tools: []Tool{{Name: "Read"}}})
	ask := zenRound(z, func(ctx context.Context, req *Request) (<-chan Event, int, string) {
		ch := make(chan Event, 8)
		for _, ev := range []Event{{Kind: KText, Text: "hi"}, {Kind: KToolStart, ID: "a", Name: "read"}, {Kind: KToolArgs, Text: "{}"},
			{Kind: KToolStart, ID: "b", Name: "shell"}, {Kind: KToolArgs, Text: "{}"}, {Kind: KStop, Stop: "tool"}} {
			ch <- ev
		}
		close(ch)
		return ch, 200, ""
	})
	in, _, _ := ask(context.Background(), &Request{})
	var got []string
	for ev := range in {
		got = append(got, ev.Name+ev.Text+ev.Stop)
	}
	if strings.Join(got, ",") != "hi,Read,{},tool" {
		t.Fatalf("got %q", got)
	}
}

// Zen's free tier refuses a core tool offered with no parameters (c9cu on
// Discord: Muse Spark 1.3 free 403 FreeTierError), and since late October
// 2026 answers 429 to 1.x's /inference/v1 shape (#3491): a free model is
// asked at OpenCode 2's /inference/openai/v1 with stubs that take OpenCode
// 2.2.5's own parameters, strict false, and a paid one stays at the
// provider's /zen/v1.
func TestZenFreeAtInferenceURL(t *testing.T) {
	s, _ := zenServer(t, chatSSE("hi", "", ""))
	var paths []string
	var sent []map[string]any
	s.client = &http.Client{Transport: countTransport(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		paths, sent = append(paths, r.URL.Host+r.URL.Path), append(sent, m)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(chatSSE("hi", "", "")))}, nil
	})}
	zenSend(t, s, "/v1/chat/completions", `{"model":"zen/mimo-v2.6-flash-free","messages":[{"role":"user","content":"hi"}]}`)
	zenSend(t, s, "/v1/chat/completions", `{"model":"zen/kimi-k2.6","messages":[{"role":"user","content":"hi"}]}`)
	if len(paths) != 2 || paths[0] != "opencode.ai/inference/openai/v1/chat/completions" || paths[1] != "opencode.ai/zen/v1/chat/completions" {
		t.Fatalf("paths %v", paths)
	}
	tools, _ := sent[0]["tools"].([]any)
	if len(tools) != 5 {
		t.Fatalf("tools %v", sent[0]["tools"])
	}
	for _, tl := range tools {
		f, _ := tl.(map[string]any)["function"].(map[string]any)
		ps, _ := f["parameters"].(map[string]any)
		if props, _ := ps["properties"].(map[string]any); len(props) == 0 {
			t.Fatalf("stub %v takes no parameter", f["name"])
		}
		if f["strict"] != false {
			t.Fatalf("stub %v strict %v", f["name"], f["strict"])
		}
	}
	// a paid model is offered nothing it didn't ask for
	if tools, _ := sent[1]["tools"].([]any); len(tools) != 0 {
		t.Fatalf("paid model offered %v", tools)
	}
}
