package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// openaiStrict is a Responses upstream that checks reasoning as OpenAI does
// (#1411): an item with its text in it (content parts) is turned away with
// the 400 the ChatGPT backend answered one with (2026-10-09), naming the
// item. With takes set it takes the text, as a relay for a DeepSeek model
// does.
type openaiStrict struct {
	takes  bool
	mu     sync.Mutex
	inputs [][]map[string]any
}

func (u *openaiStrict) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	if strings.HasSuffix(r.URL.Path, "/models") {
		io.WriteString(w, `{"models":[{"slug":"gpt-6.1-sol","visibility":"list"}]}`)
		return
	}
	var q struct {
		Input []map[string]any `json:"input"`
	}
	json.Unmarshal(b, &q)
	u.mu.Lock()
	u.inputs = append(u.inputs, q.Input)
	u.mu.Unlock()
	for i, it := range q.Input {
		if c, _ := it["content"].([]any); !u.takes && it["type"] == "reasoning" && len(c) > 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":{"message":"Invalid 'input[%d].content': array too long. Expected an array with maximum length 0, but got an array with length %d instead.","type":"invalid_request_error","param":"input[%d].content","code":"array_above_max_length"}}`, i, len(c), i)
			return
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	io.WriteString(w, sse(
		`data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"m1","role":"assistant","content":[{"type":"output_text","text":"DONE"}]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":2}}}`))
}

func (u *openaiStrict) asked() [][]map[string]any {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([][]map[string]any(nil), u.inputs...)
}

// deepseekTurn is a Codex conversation a routing group served on DeepSeek
// first: its reasoning item as #1411 shows it saved (the text and the
// ciphertext redacted as the reporter did), between the turn's call and
// its answer, and reasoning the destination sealed itself after it.
func deepseekTurn(model string) string {
	return `{"model":"` + model + `","stream":true,"store":false,"include":["reasoning.encrypted_content"],"input":[` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"count the lines"}]},` +
		`{"type":"reasoning","summary":[],"content":[{"type":"reasoning_text","text":"[redacted]"}],"encrypted_content":"[redacted]"},` +
		`{"type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"cmd\":\"wc -l main.go\"}"},` +
		`{"type":"function_call_output","call_id":"call_1","output":"42 main.go"},` +
		`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"42 lines"}]},` +
		`{"type":"reasoning","id":"rs_own","summary":[],"encrypted_content":"sealed-by-destination"},` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"and now?"}]}]}`
}

// reasoningTexts is how many reasoning items in input carry their text.
func reasoningTexts(input []map[string]any) int {
	n := 0
	for _, it := range input {
		if c, _ := it["content"].([]any); it["type"] == "reasoning" && len(c) > 0 {
			n++
		}
	}
	return n
}

// A relay that takes the text (one for a DeepSeek model) is sent the item
// as Codex sent it, and asked once.
func TestReasoningTextKeptWhereTaken(t *testing.T) {
	fresh(t)
	u := &openaiStrict{takes: true}
	srv := httptest.NewServer(u)
	t.Cleanup(srv.Close)
	if err := provider.Save(provider.Provider{ID: "ar", Name: "AgentRouter", Key: "k", Models: []string{"gpt-6-luna"}, Responses: srv.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	rec := codexPostTo(New(), deepseekTurn("ar/gpt-6-luna"))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if got := u.asked(); len(got) != 1 || reasoningTexts(got[0]) != 1 {
		t.Fatalf("asked %d times: %v", len(got), got)
	}
}

// openaiItems stands in for the ChatGPT backend and OpenAI's API as they
// check a reasoning item (jue on Discord): one with its text in it is
// turned away with the 400 the backend answers, naming the item; one
// with nothing sealed in it is looked up and not found, store being
// false (#1008). Each request's path and input are kept.
type openaiItems struct {
	mu     sync.Mutex
	paths  []string
	inputs [][]map[string]any
}

func (u *openaiItems) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	if strings.HasSuffix(r.URL.Path, "/models") {
		io.WriteString(w, `{"models":[{"slug":"gpt-6.1-sol","visibility":"list"}]}`)
		return
	}
	var q struct {
		Input []map[string]any `json:"input"`
	}
	json.Unmarshal(b, &q)
	u.mu.Lock()
	u.paths = append(u.paths, r.URL.Path)
	u.inputs = append(u.inputs, q.Input)
	u.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	for i, it := range q.Input {
		if it["type"] != "reasoning" {
			continue
		}
		if c, _ := it["content"].([]any); len(c) > 0 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":{"message":"[ArrayParam] [input[%d].content] [array_above_max_length] Invalid 'input[%d].content': array too long. Expected an array with maximum length 0, but got an array with length %d instead.","type":"invalid_request_error","param":"input[%d].content","code":"array_above_max_length"}}`, i, i, len(c), i)
			return
		}
		if enc, _ := it["encrypted_content"].(string); enc == "" {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, `{"error":{"message":"Item with id '%v' not found. Items are not persisted when `+"`store`"+` is set to false.","type":"invalid_request_error","param":"input","code":null}}`, it["id"])
			return
		}
	}
	if strings.HasSuffix(r.URL.Path, "/compact") {
		io.WriteString(w, `{"id":"resp_c","object":"response.compaction","output":[{"type":"compaction","encrypted_content":"gAAAAAcompacted"}],"usage":{"input_tokens":9,"output_tokens":3}}`)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	io.WriteString(w, sse(
		`data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"m1","role":"assistant","content":[{"type":"output_text","text":"DONE"}]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":2}}}`))
}

func (u *openaiItems) asked() ([]string, [][]map[string]any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.paths...), append([][]map[string]any(nil), u.inputs...)
}

// switchedBackFromDeepSeek is jue's Codex thread (Discord, #feedback): a
// turn on GPT, whose reasoning OpenAI sealed (content null, as Codex
// writes a reasoning item without text), then a turn on DeepSeek set for
// Codex on uni-switch's Agents page, whose reasoning DeepSeek's Responses API
// gave with its text and nothing sealed (encrypted_content null), then
// back on GPT. input[8] is DeepSeek's reasoning, with one content entry.
func switchedBackFromDeepSeek(model string) string {
	return `{"model":"` + model + `","instructions":"You are Codex.","stream":true,"store":false,"include":["reasoning.encrypted_content"],"input":[` +
		`{"type":"message","role":"developer","content":[{"type":"input_text","text":"<permissions instructions>workspace-write</permissions instructions>"}]},` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context><cwd>/Users/jue/app</cwd></environment_context>"}]},` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"看看这个仓库"}]},` +
		`{"type":"reasoning","summary":[{"type":"summary_text","text":"**Listing files**"}],"content":null,"encrypted_content":"gAAAAABo-sealed-by-openai"},` +
		`{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"ls\"}","call_id":"call_gpt1"},` +
		`{"type":"function_call_output","call_id":"call_gpt1","output":"main.go\nREADME.md"},` +
		`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"两个文件。"}]},` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"README 写了什么"}]},` +
		`{"type":"reasoning","id":"rs_ds_3b1f","summary":[],"content":[{"type":"reasoning_text","text":"用户想知道 README 的内容。"}],"encrypted_content":null},` +
		`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"README 是项目说明。"}]},` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"继续"}]}]}`
}

// jue on Discord: Codex on DeepSeek, then back on its own GPT, failed its
// next turn with OpenAI's 400 over input[8].content, the reasoning
// DeepSeek wrote. Every way a request for one of OpenAI's models reaches
// OpenAI's Responses endpoint through uni-switch — Codex's turn and its
// compaction, on its own sign-in, its API key or a uni-switch account, and a
// client of the gateway's own /v1/responses — goes without that item,
// asked once, OpenAI's own sealed reasoning and the rest kept.
func TestForeignReasoningNeverReachesOpenAI(t *testing.T) {
	for _, tc := range []struct {
		name, path, model, auth string
		spare, pauseOwn         bool
	}{
		{name: "turn on Codex's sign-in", path: CodexPath + "/responses", model: "gpt-6.1-sol", auth: "Bearer chatgpt-token"},
		{name: "compaction on Codex's sign-in", path: CodexPath + "/responses/compact", model: "gpt-6.1-sol", auth: "Bearer chatgpt-token"},
		{name: "turn on Codex's API key", path: CodexPath + "/responses", model: "gpt-6.1-sol", auth: "Bearer sk-test"},
		{name: "compaction on Codex's API key", path: CodexPath + "/responses/compact", model: "gpt-6.1-sol", auth: "Bearer sk-test"},
		{name: "turn on a uni-switch account", path: CodexPath + "/responses", model: "gpt-6.1-sol", auth: "Bearer chatgpt-token", spare: true, pauseOwn: true},
		{name: "compaction on a uni-switch account", path: CodexPath + "/responses/compact", model: "gpt-6.1-sol", auth: "Bearer chatgpt-token", spare: true, pauseOwn: true},
		{name: "the gateway's own Responses API", path: "/v1/responses", model: "codex/gpt-6.1-sol", auth: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.spare {
				codexSignedIn(t, "spare@example.com")
			} else {
				codexSignedIn(t)
			}
			if tc.pauseOwn {
				if err := provider.SetLoginOn("codex", "me@example.com", false); err != nil {
					t.Fatal(err)
				}
			}
			u := &openaiItems{}
			srv := httptest.NewServer(u)
			t.Cleanup(srv.Close)
			was, wasAPI := provider.CodexBase, codexAPIBase
			provider.CodexBase, codexAPIBase = srv.URL+"/backend-api/codex", srv.URL+"/v1"
			t.Cleanup(func() { provider.CodexBase, codexAPIBase = was, wasAPI })

			rec := httptest.NewRecorder()
			req := httptest.NewRequest("POST", tc.path, strings.NewReader(switchedBackFromDeepSeek(tc.model)))
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
				req.Header.Set("chatgpt-account-id", "acct-1")
			}
			req.Header.Set("session_id", "thread-jue")
			New().Handler().ServeHTTP(rec, req)
			if rec.Code != 200 || strings.Contains(rec.Body.String(), "array too long") {
				t.Fatalf("%d %s", rec.Code, rec.Body)
			}
			paths, got := u.asked()
			if len(got) != 1 {
				t.Fatalf("OpenAI was asked %d times (%v), want once", len(got), paths)
			}
			if reasoningTexts(got[0]) != 0 {
				t.Errorf("DeepSeek's reasoning text reached OpenAI: %v", got[0])
			}
			sealed := 0
			for _, it := range got[0] {
				if it["type"] == "reasoning" && it["encrypted_content"] == "gAAAAABo-sealed-by-openai" {
					sealed++
				}
			}
			k := kinds(got[0])
			if sealed != 1 || k["reasoning"] != 1 || k["function_call"] != 1 || k["function_call_output"] != 1 || k["message"] < 7 {
				t.Errorf("OpenAI's own items not all kept: %v", k)
			}
		})
	}
}
