package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Codex on its own ChatGPT sign-in, one account, relays a conversation
// that had a foreign provider's tool calls: their ids, two joined (86 and
// 87 characters), go as ids the backend takes, a call and its output
// alike and the same on every request, and ids that fit as they were
// (#732, congee949: thread_description turned away with 400 "input[7].
// call_id … maximum length 64").
func TestCodexRelayBoundsLongCallIDs(t *testing.T) {
	setup(t, provider.Chat, &fake{t: t})
	var got [][]byte
	chatgpt(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, b)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(`data: {"type":"response.completed","response":{"id":"r1","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`))
	})
	long := "call-" + strings.Repeat("a", 36) + "-46__fc_" + strings.Repeat("b", 36) + "_0" // 86
	other := long + "1"                                                                     // 87
	short := strings.Repeat("x", 64)
	body, _ := json.Marshal(map[string]any{"model": "gpt-6-luna", "stream": true, "input": []any{
		map[string]any{"type": "function_call", "call_id": long, "name": "shell", "arguments": "{}"},
		map[string]any{"type": "function_call_output", "call_id": long, "output": "ok"},
		map[string]any{"type": "tool_search_call", "call_id": other, "arguments": map[string]any{}},
		map[string]any{"type": "tool_search_output", "call_id": other, "tools": []any{}},
		map[string]any{"type": "custom_tool_call", "call_id": short, "name": "apply_patch", "input": "x"},
		map[string]any{"type": "custom_tool_call_output", "call_id": short, "output": "ok"},
	}})
	for range 2 {
		if code, b := codexPost(t, string(body)); code != 200 {
			t.Fatalf("%d %s", code, b)
		}
	}
	if len(got) != 2 {
		t.Fatalf("the backend was asked %d times", len(got))
	}
	ids := func(b []byte) []string {
		var q struct {
			Input []struct {
				CallID string `json:"call_id"`
			} `json:"input"`
		}
		if err := json.Unmarshal(b, &q); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, it := range q.Input {
			out = append(out, it.CallID)
		}
		return out
	}
	a, b := ids(got[0]), ids(got[1])
	if len(a) != 6 || strings.Join(a, ",") != strings.Join(b, ",") {
		t.Fatalf("ids %v then %v", a, b)
	}
	for _, id := range a {
		if id == "" || len(id) > 64 {
			t.Errorf("an id the backend refuses: %q", id)
		}
	}
	if a[0] != a[1] || a[2] != a[3] || a[0] == a[2] || a[4] != short || a[5] != short {
		t.Errorf("ids %v", a)
	}
}
