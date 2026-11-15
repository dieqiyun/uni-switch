package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// deepseekResponses plays OpenCode Go's /responses for deepseek-v4.1-flash
// in thinking mode: a request with a reasoning item that carries no
// reasoning_text is refused with the 400 #1104's reporter saw, word for
// word. The error names the input, so it reads as one over an item.
type deepseekResponses struct {
	mu     sync.Mutex
	inputs [][]map[string]any
}

func (u *deepseekResponses) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var q struct {
		Input []map[string]any `json:"input"`
	}
	json.Unmarshal(body, &q)
	u.mu.Lock()
	u.inputs = append(u.inputs, q.Input)
	u.mu.Unlock()
	if !deepseekTakesInput(q.Input) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"message":"The `+"`reasoning_text`"+` in the thinking mode must be passed back to the API.","type":"invalid_request_error","param":"input","code":"invalid_request_error"}}`)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	io.WriteString(w, sse(
		`data: {"type":"response.created","response":{"id":"resp_ds","status":"in_progress"}}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"m1","role":"assistant","content":[{"type":"output_text","text":"done"}]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_ds","status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":2}}}`))
}

// deepseekTakesInput is whether api.deepseek.com's /v1/responses
// (deepseek-flash, high) takes a request's input in thinking mode: every
// reasoning item has a reasoning_text that isn't empty (#1104), and each
// step of the turn being continued (the model's reasoning, what it said and
// its calls, between a user message or a tool output and the next) that
// makes a call (#1592) or follows a tool output starts with a reasoning
// item. A step that said something before its reasoning, or with none
// before its call, is refused (#1822). Checked 2026-10-11 and 2026-10-12:
// [text, reasoning, call] and [output, text] are refused, [reasoning, text,
// call] is taken, and a step before the last user message isn't looked at.
func deepseekTakesInput(input []map[string]any) bool {
	turn := -1
	for i, it := range input {
		if it["role"] == "user" {
			turn = i
		}
	}
	start, lead, called, afterOut := -1, false, false, false
	stepOK := func() bool { return start < turn || lead || !called && !afterOut }
	for i, it := range input {
		typ, _ := it["type"].(string)
		role, _ := it["role"].(string)
		if typ == "reasoning" {
			ok := false
			content, _ := it["content"].([]any)
			for _, c := range content {
				if m, _ := c.(map[string]any); m["type"] == "reasoning_text" && m["text"] != "" {
					ok = true
				}
			}
			if !ok {
				return false
			}
		}
		if typ == "reasoning" || typ == "function_call" || typ == "custom_tool_call" || (typ == "message" || typ == "") && role == "assistant" {
			if start < 0 {
				start, lead, called = i, typ == "reasoning", false
			}
			called = called || strings.HasSuffix(typ, "_call")
			continue
		}
		if start >= 0 && !stepOK() {
			return false
		}
		start = -1
		switch {
		case strings.HasSuffix(typ, "_output"):
			afterOut = true
		case role == "user":
			afterOut = false
		}
	}
	return start < 0 || stepOK()
}

func (u *deepseekResponses) last() []map[string]any {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.inputs[len(u.inputs)-1]
}

// TestDeepSeekReasoningPassedThrough (#1104): Codex on OpenCode Go's
// deepseek-v4.1-flash, its request passed through as it is. Codex hands a
// reasoning item back as it got it (codex-rs keeps reasoning_text content
// when it has some, and sends content and encrypted_content as null
// otherwise), so an item from a turn uni-switch translated, or another provider
// in a mixed group served, has only its summary. Passed on as it was,
// DeepSeek refused the request; and the refusal, naming the input, made
// uni-switch leave every reasoning item out of the requests after it.
func TestDeepSeekReasoningPassedThrough(t *testing.T) {
	fresh(t)
	up := &deepseekResponses{}
	srv := httptest.NewServer(up)
	defer srv.Close()
	if err := provider.Save(provider.Provider{ID: "opencode-go", Name: "OpenCode Go", Key: "k", Models: []string{"deepseek-v4.1-flash"}, Responses: srv.URL + "/zen/go/v1"}); err != nil {
		t.Fatal(err)
	}
	ask := func(reasoning string) (int, string) {
		body := `{"model":"opencode-go/deepseek-v4.1-flash","stream":true,"store":false,"reasoning":{"effort":"max","summary":"auto"},"include":["reasoning.encrypted_content"],
		  "tools":[{"type":"function","name":"shell","parameters":{"type":"object","properties":{"command":{"type":"array","items":{"type":"string"}}}}}],"input":[
		  {"type":"message","role":"user","content":[{"type":"input_text","text":"fix the login bug"}]},
		  ` + reasoning + `,
		  {"type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"command\":[\"ls\"]}"},
		  {"type":"function_call_output","call_id":"call_1","output":"main.go"}]}`
		return post(t, "/v1/responses", body)
	}
	reasoningText := func(input []map[string]any) string {
		for _, it := range input {
			if it["type"] == "reasoning" {
				content, _ := it["content"].([]any)
				for _, c := range content {
					if m, _ := c.(map[string]any); m["type"] == "reasoning_text" {
						s, _ := m["text"].(string)
						return s
					}
				}
			}
		}
		return ""
	}

	// a summary only, as uni-switch gives Codex in a translated reply
	if code, b := ask(`{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"Looking at main.go"}],"content":null,"encrypted_content":null}`); code != 200 {
		t.Fatalf("summary-only reasoning: %d %s", code, b)
	}
	if got := reasoningText(up.last()); got != "Looking at main.go" {
		t.Fatalf("reasoning_text sent = %q, want the summary", got)
	}

	// nothing to give back: it goes with a space, which DeepSeek takes
	if code, b := ask(`{"type":"reasoning","id":"rs_2","summary":[],"content":null,"encrypted_content":null}`); code != 200 {
		t.Fatalf("a reasoning item with nothing in it: %d %s", code, b)
	}
	if got := reasoningText(up.last()); got != noReasoning {
		t.Fatalf("an empty item's reasoning_text sent = %q, want %q", got, noReasoning)
	}
	// the turn's own reasoning, which DeepSeek gave, goes back as it was
	if code, b := ask(`{"type":"reasoning","id":"rs_3","summary":[],"content":[{"type":"reasoning_text","text":"I should list the files first."}],"encrypted_content":null}`); code != 200 {
		t.Fatalf("DeepSeek's own reasoning: %d %s", code, b)
	}
	if got := reasoningText(up.last()); got != "I should list the files first." {
		t.Fatalf("DeepSeek's own reasoning_text sent = %q", got)
	}
}

// Translated to a Responses upstream (Codex's web_search to DeepSeek's
// own API, #388), a DeepSeek model's tool call with no thinking goes after
// a reasoning item all the same (#1592); one that thought keeps its own,
// and another model behind a relay is sent none.
func TestTranslatedDeepSeekCallWithoutReasoning(t *testing.T) {
	r, err := parseResponses([]byte(`{"model":"m","input":[
		{"type":"message","role":"user","content":"hi"},
		{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"gAAAAABpZm9vYmFy"},
		{"type":"function_call","call_id":"c1","name":"sh","arguments":"{}"},
		{"type":"function_call_output","call_id":"c1","output":"ok"},
		{"type":"reasoning","summary":[{"type":"summary_text","text":"Once more."}]},
		{"type":"function_call","call_id":"c2","name":"sh","arguments":"{}"},
		{"type":"function_call_output","call_id":"c2","output":"ok"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var q struct {
		Input []struct {
			Type    string  `json:"type"`
			Content []rText `json:"content"`
		} `json:"input"`
	}
	json.Unmarshal(buildResponses(r, "deepseek-flash", "api.deepseek.com", false), &q)
	var got []string
	for _, it := range q.Input {
		s := it.Type
		if it.Type == "reasoning" && len(it.Content) == 1 {
			s += "=" + it.Content[0].Text
		}
		got = append(got, s)
	}
	if want := "message,reasoning= ,function_call,function_call_output,reasoning=Once more.,function_call,function_call_output"; strings.Join(got, ",") != want {
		t.Fatalf("sent %s\nwant %s", strings.Join(got, ","), want)
	}
	if b := buildResponses(r, "gpt-5.4", "relay.example", false); strings.Contains(string(b), `"reasoning_text"`) {
		t.Fatalf("gpt-5.4 was given reasoning: %s", b)
	}
}
