package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// eventLine frames one line of a Responses SSE stream.
func eventLine(line string) string {
	var t struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal([]byte(line), &t)
	return fmt.Sprintf("event: %s\ndata: %s\n\n", t.Type, line)
}

// responsesCallsUpstream serves a Responses stream of two function_call
// items whose added events come before any of their arguments, laid out by
// calls: each call's delta, done and item done, one after another.
func responsesCallsUpstream(t *testing.T, lines []string) {
	t.Helper()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, l := range lines {
			io.WriteString(w, eventLine(l))
		}
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "up", Name: "UP", Key: "k", Models: []string{"gpt-5.5"}, Responses: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
}

// readChatCalls reads a Chat stream's tool calls as id, name and the
// arguments each index was given.
func readChatCalls(t *testing.T, body string) string {
	t.Helper()
	type out struct{ id, name, args string }
	var calls []out
	for _, ev := range events(body) {
		for _, c := range choices(ev) {
			d, _ := c["delta"].(map[string]any)
			parts, _ := d["tool_calls"].([]any)
			for _, x := range parts {
				tc, _ := x.(map[string]any)
				i, _ := tc["index"].(float64)
				for len(calls) <= int(i) {
					calls = append(calls, out{})
				}
				o := &calls[int(i)]
				if id, _ := tc["id"].(string); id != "" {
					o.id = id
				}
				fn, _ := tc["function"].(map[string]any)
				if name, _ := fn["name"].(string); name != "" {
					o.name = name
				}
				args, _ := fn["arguments"].(string)
				o.args += args
			}
		}
	}
	var got strings.Builder
	for _, c := range calls {
		fmt.Fprintf(&got, "%s %s %s\n", c.id, c.name, c.args)
	}
	return got.String()
}

const wantTwoCalls = "call_1 read_file {\"path\": \"a.py\"}\ncall_2 read_file {\"path\": \"b.py\"}\n"

func responsesCallLine(kind, itemID string, index int, args string) string {
	switch kind {
	case "added":
		return fmt.Sprintf(`{"type":"response.output_item.added","output_index":%d,"item":{"type":"function_call","id":%q,"call_id":"call_%d","name":"read_file","arguments":""}}`, index, itemID, index+1)
	case "delta":
		return fmt.Sprintf(`{"type":"response.function_call_arguments.delta","item_id":%q,"output_index":%d,"delta":%s}`, itemID, index, args)
	case "done":
		return fmt.Sprintf(`{"type":"response.function_call_arguments.done","item_id":%q,"output_index":%d,"arguments":%s}`, itemID, index, args)
	case "itemdone":
		return fmt.Sprintf(`{"type":"response.output_item.done","output_index":%d,"item":{"type":"function_call","id":%q,"call_id":"call_%d","name":"read_file","arguments":%s}}`, index, itemID, index+1, args)
	}
	return ""
}

func responsesCompleted() string {
	return `{"type":"response.completed","response":{"id":"resp_1","model":"gpt-5.5","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30}}}`
}

// A Responses upstream that adds both function_call items before either's
// arguments (#3985, built by hand, not captured): each delta and done
// names its item_id and output_index, and a Chat client should get two
// calls, each with its own arguments.
func TestResponsesBatchedCallsToChat(t *testing.T) {
	fresh(t)
	responsesCallsUpstream(t, []string{
		`{"type":"response.created","response":{"id":"resp_1","model":"gpt-5.5","status":"in_progress","output":[]}}`,
		responsesCallLine("added", "fc_1", 0, ""),
		responsesCallLine("added", "fc_2", 1, ""),
		responsesCallLine("delta", "fc_1", 0, jsonStr(`{"path": "a.py"}`)),
		responsesCallLine("done", "fc_1", 0, jsonStr(`{"path": "a.py"}`)),
		responsesCallLine("itemdone", "fc_1", 0, jsonStr(`{"path": "a.py"}`)),
		responsesCallLine("delta", "fc_2", 1, jsonStr(`{"path": "b.py"}`)),
		responsesCallLine("done", "fc_2", 1, jsonStr(`{"path": "b.py"}`)),
		responsesCallLine("itemdone", "fc_2", 1, jsonStr(`{"path": "b.py"}`)),
		responsesCompleted(),
	})
	code, body := post(t, "/v1/chat/completions", `{"model":"up/gpt-5.5","stream":true,"messages":[{"role":"user","content":"Read a.py and b.py."}],"tools":[{"type":"function","function":{"name":"read_file","parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}}]}`)
	if code != 200 {
		t.Fatalf("status %d: %s", code, body)
	}
	if got := readChatCalls(t, body); got != wantTwoCalls {
		t.Fatalf("tool calls:\n%swant:\n%s", got, wantTwoCalls)
	}
}

// The same upstream with the two calls' arguments by turns: each call
// still gets its own.
func TestResponsesBatchedCallsByTurnsToChat(t *testing.T) {
	fresh(t)
	responsesCallsUpstream(t, []string{
		`{"type":"response.created","response":{"id":"resp_1","model":"gpt-5.5","status":"in_progress","output":[]}}`,
		responsesCallLine("added", "fc_1", 0, ""),
		responsesCallLine("added", "fc_2", 1, ""),
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":0,"delta":"{\"path\":"}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_2","output_index":1,"delta":"{\"path\":"}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":0,"delta":" \"a.py\"}"}`,
		responsesCallLine("done", "fc_1", 0, jsonStr(`{"path": "a.py"}`)),
		responsesCallLine("itemdone", "fc_1", 0, jsonStr(`{"path": "a.py"}`)),
		`{"type":"response.function_call_arguments.delta","item_id":"fc_2","output_index":1,"delta":" \"b.py\"}"}`,
		responsesCallLine("done", "fc_2", 1, jsonStr(`{"path": "b.py"}`)),
		responsesCallLine("itemdone", "fc_2", 1, jsonStr(`{"path": "b.py"}`)),
		responsesCompleted(),
	})
	code, body := post(t, "/v1/chat/completions", `{"model":"up/gpt-5.5","stream":true,"messages":[{"role":"user","content":"Read a.py and b.py."}],"tools":[{"type":"function","function":{"name":"read_file","parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}}]}`)
	if code != 200 {
		t.Fatalf("status %d: %s", code, body)
	}
	if got := readChatCalls(t, body); got != wantTwoCalls {
		t.Fatalf("tool calls:\n%swant:\n%s", got, wantTwoCalls)
	}
}

// A Responses upstream in its own order — an item added, argued and done
// before the next — reads as it did before (#3985's fix).
func TestResponsesOwnOrderUnchanged(t *testing.T) {
	fresh(t)
	responsesCallsUpstream(t, []string{
		`{"type":"response.created","response":{"id":"resp_1","model":"gpt-5.5","status":"in_progress","output":[]}}`,
		responsesCallLine("added", "fc_1", 0, ""),
		responsesCallLine("delta", "fc_1", 0, jsonStr(`{"path": "a.py"}`)),
		responsesCallLine("done", "fc_1", 0, jsonStr(`{"path": "a.py"}`)),
		responsesCallLine("itemdone", "fc_1", 0, jsonStr(`{"path": "a.py"}`)),
		responsesCallLine("added", "fc_2", 1, ""),
		responsesCallLine("delta", "fc_2", 1, jsonStr(`{"path": "b.py"}`)),
		responsesCallLine("done", "fc_2", 1, jsonStr(`{"path": "b.py"}`)),
		responsesCallLine("itemdone", "fc_2", 1, jsonStr(`{"path": "b.py"}`)),
		responsesCompleted(),
	})
	code, body := post(t, "/v1/chat/completions", `{"model":"up/gpt-5.5","stream":true,"messages":[{"role":"user","content":"Read a.py and b.py."}],"tools":[{"type":"function","function":{"name":"read_file","parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}}]}`)
	if code != 200 {
		t.Fatalf("status %d: %s", code, body)
	}
	if got := readChatCalls(t, body); got != wantTwoCalls {
		t.Fatalf("tool calls:\n%swant:\n%s", got, wantTwoCalls)
	}
}
