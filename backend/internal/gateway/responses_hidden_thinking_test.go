package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/claudeseal"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A Claude turn that calls a tool and then thinks without showing it, a
// redacted_thinking block or a thinking block signed with no text, is a
// whole Responses stream for Codex: the call, then response.completed. The
// encoder read the open call from the collector's last part, which by then
// was the thinking, and panicked, so the client got a chunked body cut short
// ("unexpected EOF", "stream disconnected before completion", #1786).
func TestResponsesStreamToolThenHiddenThinking(t *testing.T) {
	const head = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_01\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-opus-4-6\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":12,\"output_tokens\":1}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_01\",\"name\":\"shell\",\"input\":{}}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"command\\\":[\\\"ls\\\"]}\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"
	const tail = "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":30}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	for _, c := range []struct {
		name, block string
		visible     bool
	}{
		{"redacted_thinking", "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"redacted_thinking\",\"data\":\"EmwKAhgBEgy3va3pzix\"}}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n", false},
		{"empty thinking, signed at its start", "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"fixture\"}}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n", false},
		{"empty thinking, signed in a delta", "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"fixture\"}}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n", false},
		{"visible thinking", "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"Now list it.\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"fixture\"}}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			fresh(t)
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, head+c.block+tail)
			}))
			defer up.Close()
			if err := provider.Save(provider.Provider{ID: "anth", Name: "Anth", Key: "k", Anthropic: up.URL, Models: []string{"claude-opus-4-6"}}); err != nil {
				t.Fatal(err)
			}
			gw := httptest.NewServer(New().Handler())
			defer gw.Close()
			resp, err := http.Post(gw.URL+"/v1/responses", "application/json", strings.NewReader(
				`{"model":"anth/claude-opus-4-6","stream":true,"input":"list the files","reasoning":{"effort":"high"},`+
					`"tools":[{"type":"function","name":"shell","parameters":{"type":"object","properties":{"command":{"type":"array","items":{"type":"string"}}}}}]}`))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("status %d, the stream was cut short: %v\n%s", resp.StatusCode, err, body)
			}
			var completed map[string]any
			var call map[string]any
			var reasoning []map[string]any
			for _, ev := range events(string(body)) {
				switch ev["type"] {
				case "response.completed":
					completed = ev
				case "response.output_item.done":
					item, _ := ev["item"].(map[string]any)
					switch item["type"] {
					case "function_call":
						call = item
					case "reasoning":
						reasoning = append(reasoning, item)
					}
				}
			}
			if completed == nil {
				t.Fatalf("no response.completed:\n%s", body)
			}
			if call == nil || call["call_id"] != "toolu_01" || call["name"] != "shell" || call["arguments"] != `{"command":["ls"]}` {
				b, _ := json.Marshal(call)
				t.Fatalf("function_call = %s, want toolu_01 shell {\"command\":[\"ls\"]}\n%s", b, body)
			}
			// Thinking Claude didn't show is still given back to it on the
			// next request (mysterious_dove_96933 on Discord): one reasoning
			// item carrying the block, its summary empty unless shown.
			if len(reasoning) != 1 {
				t.Fatalf("%d reasoning items, want 1\n%s", len(reasoning), body)
			}
			enc, _ := reasoning[0]["encrypted_content"].(string)
			if !claudeseal.Is(enc) {
				t.Errorf("reasoning item's encrypted_content = %q, want Claude's block sealed", enc)
			}
			summary, _ := reasoning[0]["summary"].([]any)
			if shown := len(summary) > 0; shown != c.visible {
				t.Errorf("reasoning summary = %v, want shown %v", summary, c.visible)
			}
		})
	}
}
