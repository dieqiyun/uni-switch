package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Search events already carry the query and sources before the client
// reply is encoded. Text alone cannot tell a Responses client that a
// search ran, or which sources it found.
func TestResponsesSearchResults(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "JSON", true: "SSE"}[stream], func(t *testing.T) {
			body, events := responsesSearchReply(t, stream,
				Event{Kind: KStart, MsgID: "search_reply", Model: "deepseek", Usage: Usage{Input: 12}},
				Event{Kind: KText, Text: "Checking the release notes."},
				Event{Kind: KSearch, Text: "nerdctl CDI support", Hits: []Hit{
					{Title: "Releases", URL: "https://github.com/containerd/nerdctl/releases"},
					{Title: "A hit without a URL"},
					{Title: "CDI", URL: "https://github.com/containerd/nerdctl/blob/main/docs/cdi.md"},
				}},
				Event{Kind: KSearch, Text: "another query"},
				Event{Kind: KText, Text: "Here are the results."},
				Event{Kind: KToolStart, ID: "call_read", Name: "read_file"},
				Event{Kind: KToolArgs, Text: `{"path":"README.md"}`},
				Event{Kind: KUsage, Usage: Usage{Output: 8}},
				Event{Kind: KStop, Stop: "tool"},
			)
			output := body["output"].([]any)
			var kinds []string
			ids := map[string]bool{}
			for _, raw := range output {
				item := raw.(map[string]any)
				kinds = append(kinds, item["type"].(string))
				id, _ := item["id"].(string)
				if id == "" || ids[id] || item["status"] != "completed" {
					t.Fatalf("output items need distinct IDs and completed status: %v", output)
				}
				ids[id] = true
			}
			if !reflect.DeepEqual(kinds, []string{"message", "web_search_call", "web_search_call", "message", "function_call"}) {
				t.Fatalf("search output lost or reordered: got %v", kinds)
			}
			for i, query := range []string{"nerdctl CDI support", "another query"} {
				item := output[i+1].(map[string]any)
				action := item["action"].(map[string]any)
				if !strings.HasPrefix(item["id"].(string), "ws_") || action["type"] != "search" || action["query"] != query || !reflect.DeepEqual(action["queries"], []any{query}) {
					t.Fatalf("search action = %v", item)
				}
				sources, ok := action["sources"].([]any)
				if !ok {
					t.Fatalf("sources must be a list, even with no hits: %v", action)
				}
				want := []any{}
				if i == 0 {
					want = []any{
						map[string]any{"type": "url", "url": "https://github.com/containerd/nerdctl/releases"},
						map[string]any{"type": "url", "url": "https://github.com/containerd/nerdctl/blob/main/docs/cdi.md"},
					}
				}
				if !reflect.DeepEqual(sources, want) {
					t.Fatalf("search sources = %v, want %v", sources, want)
				}
			}
			for i, text := range map[int]string{0: "Checking the release notes.", 3: "Here are the results."} {
				content := output[i].(map[string]any)["content"].([]any)[0].(map[string]any)
				if content["text"] != text || len(content["annotations"].([]any)) != 0 {
					t.Fatalf("text changed or citations invented: %v", content)
				}
			}
			call := output[4].(map[string]any)
			if call["call_id"] != "call_read" || call["name"] != "read_file" || call["arguments"] != `{"path":"README.md"}` {
				t.Fatalf("client tool call changed: %v", call)
			}
			usage := body["usage"].(map[string]any)
			if body["status"] != "completed" || usage["input_tokens"] != float64(12) || usage["output_tokens"] != float64(8) {
				t.Fatalf("status or usage changed: %v", body)
			}
			if stream {
				added, done, searched := 0, 0, 0
				for seq, ev := range events {
					if ev["sequence_number"] != float64(seq) {
						t.Fatalf("event sequence broken: %v", ev)
					}
					switch ev["type"] {
					case "response.output_item.added":
						if ev["output_index"] != float64(added) {
							t.Fatalf("added item index = %v, want %d", ev, added)
						}
						if ev["item"].(map[string]any)["id"] != output[added].(map[string]any)["id"] {
							t.Fatalf("item ID changed at completion: %v", ev)
						}
						added++
					case "response.web_search_call.completed":
						index := searched + 1
						if ev["output_index"] != float64(index) || ev["item_id"] != output[index].(map[string]any)["id"] {
							t.Fatalf("search completion points to another item: %v", ev)
						}
						searched++
					case "response.output_item.done":
						if ev["output_index"] != float64(done) || !reflect.DeepEqual(ev["item"], output[done]) {
							t.Fatalf("done item missing from final output: %v", ev)
						}
						done++
					}
				}
				if added != 5 || done != 5 || searched != 2 {
					t.Fatalf("added=%d done=%d searches=%d", added, done, searched)
				}
			}
		})
	}
}

func TestResponsesSearchDecodedResults(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "JSON", true: "SSE"}[stream], func(t *testing.T) {
			// A Responses provider's search, decoded before its reply is
			// translated. The terminal output may be empty (Codex).
			chunks := []string{
				`{"type":"response.created","response":{"id":"resp_search","model":"m"}}`,
				`{"type":"response.output_item.done","item":{"id":"ws_upstream","type":"web_search_call","status":"completed","action":{"type":"search","query":"nerdctl CDI","sources":[{"type":"url","url":"https://github.com/containerd/nerdctl/releases"}]}}}`,
				`{"type":"response.output_text.delta","delta":"Found the release notes."}`,
				`{"type":"response.completed","response":{"id":"resp_search","status":"completed","output":[],"usage":{"input_tokens":12,"output_tokens":8}}}`,
			}
			var input []Event
			decode := decoder(provider.Responses)
			for _, chunk := range chunks {
				if err := decode(chunk, func(ev Event) { input = append(input, ev) }); err != nil {
					t.Fatal(err)
				}
			}
			body, _ := responsesSearchReply(t, stream, input...)
			output := body["output"].([]any)
			if len(output) != 2 || output[0].(map[string]any)["type"] != "web_search_call" || output[1].(map[string]any)["type"] != "message" {
				t.Fatalf("decoded provider search missing: %v", output)
			}
			action := output[0].(map[string]any)["action"].(map[string]any)
			if action["query"] != "nerdctl CDI" || !reflect.DeepEqual(action["sources"], []any{map[string]any{"type": "url", "url": "https://github.com/containerd/nerdctl/releases"}}) {
				t.Fatalf("decoded search lost query or sources: %v", action)
			}
		})
	}
}

func TestResponsesSearchFollowedByFailure(t *testing.T) {
	body, events := responsesSearchReply(t, true,
		Event{Kind: KStart, MsgID: "search_then_error"},
		Event{Kind: KSearch, Text: "nerdctl CDI", Hits: []Hit{{URL: "https://github.com/containerd/nerdctl/releases"}}},
		Event{Kind: KError, Text: "upstream failed after searching", Code: "server_error"},
	)
	output := body["output"].([]any)
	if body["status"] != "failed" || body["error"] == nil || len(output) != 1 || output[0].(map[string]any)["type"] != "web_search_call" {
		t.Fatalf("completed search or subsequent error lost: %v", body)
	}
	for _, ev := range events {
		if ev["type"] == "response.completed" {
			t.Fatal("a completed search must not make a failed response successful")
		}
	}
}

func TestResponsesSearchNotInvented(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, failed := range []bool{false, true} {
			t.Run(map[bool]string{false: "JSON", true: "SSE"}[stream]+"/"+map[bool]string{false: "text only", true: "search failed"}[failed], func(t *testing.T) {
				input := []Event{{Kind: KStart, MsgID: "no_search"}}
				if failed {
					input = append(input, Event{Kind: KError, Text: "search unavailable", Status: 503})
				} else {
					input = append(input, Event{Kind: KText, Text: "I know https://example.com/ from memory."}, Event{Kind: KStop, Stop: "stop"})
				}
				body, events := responsesSearchReply(t, stream, input...)
				data, err := json.Marshal([]any{body, events})
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(data), "web_search_call") {
					t.Fatalf("a search was invented: %s", data)
				}
				if failed && body["error"] == nil {
					t.Fatalf("search failure was hidden: %v", body)
				}
			})
		}
	}
}

// Exercise the relay's real encoder/collector selection without a live
// provider, credentials, or network server.
func responsesSearchReply(t *testing.T, stream bool, input ...Event) (map[string]any, []map[string]any) {
	t.Helper()
	req, err := parseResponses([]byte(`{"model":"deep/deepseek","input":"Search the web","tools":[{"type":"web_search"}],"include":["web_search_call.action.sources"]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Stream = stream
	events := make(chan Event, len(input))
	for _, ev := range input {
		events <- ev
	}
	close(events)
	rec := httptest.NewRecorder()
	relay(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", nil), provider.Responses, "Test provider", req, events, &Usage{}, func() {}, nil, func(string, string, bool) {})
	var body map[string]any
	if !stream || rec.Code != http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body, nil
	}
	var wire []map[string]any
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			var ev map[string]any
			if err := json.Unmarshal([]byte(data), &ev); err != nil {
				t.Fatal(err)
			}
			wire = append(wire, ev)
			if ev["type"] == "response.completed" || ev["type"] == "response.failed" {
				body = ev["response"].(map[string]any)
			}
		}
	}
	if body == nil {
		t.Fatalf("missing terminal response: %s", rec.Body.String())
	}
	return body, wire
}

// A client sends the search item back with the rest of the turn (Codex
// replays its output as the next input). It must not break the request
// or lose the turn's other items.
func TestResponsesSearchItemReplayed(t *testing.T) {
	body, _ := responsesSearchReply(t, false,
		Event{Kind: KStart, MsgID: "replay"},
		Event{Kind: KSearch, Text: "nerdctl CDI", Hits: []Hit{{URL: "https://github.com/containerd/nerdctl/releases"}}},
		Event{Kind: KText, Text: "Found it."},
		Event{Kind: KStop, Stop: "stop"},
	)
	input := append([]any{map[string]any{"role": "user", "content": "Search the web"}}, body["output"].([]any)...)
	input = append(input, map[string]any{"role": "user", "content": "Thanks"})
	raw, err := json.Marshal(map[string]any{"model": "deep/deepseek", "input": input, "tools": []any{map[string]any{"type": "web_search"}}})
	if err != nil {
		t.Fatal(err)
	}
	req, err := parseResponses(raw)
	if err != nil {
		t.Fatalf("a replayed search item broke the request: %v", err)
	}
	var said []string
	for _, m := range req.Messages {
		said = append(said, m.Role+":"+text(m.Parts))
	}
	if !reflect.DeepEqual(said, []string{"user:Search the web", "assistant:Found it.", "user:Thanks"}) {
		t.Fatalf("turn changed by a replayed search item: %q", said)
	}
}
