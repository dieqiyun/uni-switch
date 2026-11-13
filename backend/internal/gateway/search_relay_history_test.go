package gateway

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// commandCodeChat stands in for Command Code's Provider API, which turns a
// Responses request for a chat model into chat: a web_search_call in the
// input becomes an assistant tool call nothing answers, and DeepSeek
// refuses it, as it refused mxmllln's summary request (Discord,
// deepseek-v4-flash-vision-exp). It keeps the bodies it was sent.
func commandCodeChat(t *testing.T, models ...string) *[][]byte {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var mu sync.Mutex
	got := &[][]byte{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		*got = append(*got, b)
		mu.Unlock()
		if bytes.Contains(b, []byte(`"web_search_call"`)) && bytes.Contains(b, []byte("deepseek")) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			io.WriteString(w, `{"error":{"message":"An assistant message with 'tool_calls' must be followed by tool messages responding to each 'tool_call_id'. The following tool_call_ids did not have response messages: ws_0c7e789f31e92725016ac8f91213cc87d0ac350b2fd39a94a2","type":"invalid_request_error"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`data: {"type":"response.created","response":{"id":"resp_1"}}`,
			`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"The user asked for the newest Go release; it is Go 1.27.2."}]}}`,
			`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":9,"output_tokens":2}}}`))
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "commandcode", Name: "Command Code", Preset: "commandcode", Key: "k", Models: models,
		Chat: up.URL + "/provider/v1", Responses: up.URL + "/provider/v1", Anthropic: up.URL + "/provider"}); err != nil {
		t.Fatal(err)
	}
	return got
}

// a function call and its output beside the search, as in the thread
const searchedWithCalls = `{"type":"function_call","call_id":"call_1","name":"exec_command","arguments":"{\"cmd\":\"go version\"}"},` +
	`{"type":"function_call_output","call_id":"call_1","output":"go version go1.26.8 darwin/arm64"},` + searchedHistory

// A thread that searched the web, compacted on a model its provider
// serves on Responses and runs no web search for: the summary request
// offers no web_search, so it was relayed as Codex sent it, and the
// provider's chat form of the web_search_call refused the summary.
// Codex's compaction_trigger (uni-switch's summary request) and a summary
// request Codex makes itself (tools: [], #1270) each go without the
// searches; the calls and their outputs go as they were.
func TestSearchedThreadCompactsOnARelayThatDoesntSearch(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"compaction_trigger", `{"model":"commandcode/deepseek/deepseek-v4-flash-vision-exp","stream":true,"store":false,
			"tools":[{"type":"function","name":"exec_command","parameters":{"type":"object"}},{"type":"web_search"}],
			"input":[` + searchedWithCalls + `,{"type":"compaction_trigger"}]}`},
		{"Codex's own summary request", `{"model":"commandcode/deepseek/deepseek-v4-flash-vision-exp","stream":true,"store":false,"tools":[],"tool_choice":"auto",
			"input":[` + searchedWithCalls + `,{"type":"message","role":"user","content":[{"type":"input_text","text":"You are performing a CONTEXT CHECKPOINT COMPACTION. Create a handoff summary for another LLM that will resume the task."}]}]}`},
		{"a turn with search off", `{"model":"commandcode/deepseek/deepseek-v4-flash-vision-exp","stream":true,"store":false,
			"tools":[{"type":"function","name":"exec_command","parameters":{"type":"object"}}],
			"input":[` + searchedWithCalls + `,{"type":"message","role":"user","content":[{"type":"input_text","text":"and the one before?"}]}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := commandCodeChat(t, "deepseek/deepseek-v4-flash-vision-exp")
			code, body := post(t, "/v1/responses", tc.body)
			if code != 200 || !strings.Contains(body, "response.completed") || strings.Contains(body, "tool_calls") {
				t.Fatalf("status %d: %s", code, body)
			}
			if len(*got) == 0 {
				t.Fatal("nothing reached the provider")
			}
			for _, b := range *got {
				if bytes.Contains(b, []byte(`"web_search_call"`)) {
					t.Fatalf("a search went to a provider that runs none: %s", b)
				}
				if !bytes.Contains(b, []byte(`"call_id":"call_1"`)) || !bytes.Contains(b, []byte("go1.26.8")) {
					t.Fatalf("the call or its output was lost: %s", b)
				}
			}
		})
	}
}

// A model the provider runs web search for (Command Code's gpt-6-luna on
// Responses, #1590) reads its searches back as they were.
func TestSearchedThreadKeepsItsSearchesWhereTheProviderSearches(t *testing.T) {
	got := commandCodeChat(t, "gpt-6-luna")
	code, body := post(t, "/v1/responses", `{"model":"commandcode/gpt-6-luna","stream":true,"store":false,"tools":[],
		"input":[`+searchedWithCalls+`,{"type":"message","role":"user","content":[{"type":"input_text","text":"summarise"}]}]}`)
	if code != 200 {
		t.Fatalf("status %d: %s", code, body)
	}
	if len(*got) != 1 || !bytes.Contains((*got)[0], []byte(`"web_search_call"`)) {
		t.Fatalf("the search was left out where the provider reads it: %q", *got)
	}
}

func TestWithoutSearchCalls(t *testing.T) {
	for _, b := range []string{`{"input":"hi"}`, `{"input":[{"type":"message","role":"user","content":"hi"}]}`, `not json "web_search_call"`} {
		if got := string(withoutSearchCalls([]byte(b))); got != b {
			t.Errorf("%s became %s", b, got)
		}
	}
	got := string(withoutSearchCalls([]byte(`{"model":"m","input":[` + searchedHistory + `]}`)))
	if strings.Contains(got, "web_search_call") || !strings.Contains(got, "Go 1.27.2") || !strings.Contains(got, "go.dev for the latest") {
		t.Fatalf("%s", got)
	}
}
