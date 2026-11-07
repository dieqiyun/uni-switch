package gateway

import (
	"encoding/json"
	"os"
	"testing"
)

// These expected values come from executing the pinned reference functions in
// an isolated program, not from this implementation or a Magpie dependency.
func TestPinnedCoreDifferentialVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/core-reference-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors map[string][][]any
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 5 {
		t.Fatal("incomplete core oracle")
	}
	for kind, rows := range vectors {
		for _, v := range rows {
			var got, want any
			switch kind {
			case "effort":
				got, want = effort(v[0].(string)), v[1]
			case "budget":
				got, want = budget(v[0].(string)), v[1]
			case "budgetEffort":
				got, want = effortBudget(int(v[0].(float64))), v[1]
			case "desktopNumber":
				got, want = desktopNumber(v[0].(string)), v[1]
			case "historyID":
				messageAt, callAt := int(v[0].(float64)), int(v[1].(float64))
				messages, calls := []any{}, []any{}
				for i := 0; i < messageAt; i++ {
					messages = append(messages, Object{"role": "user", "content": "earlier"})
				}
				for i := 0; i < callAt; i++ {
					calls = append(calls, Object{"id": "provided", "function": Object{"name": "f", "arguments": "{}"}})
				}
				calls = append(calls, Object{"function": Object{"name": v[2], "arguments": v[3]}})
				body := Object{"messages": append(messages, Object{"role": "assistant", "tool_calls": calls})}
				repairChatHistory(body)
				got, want = field(calls[callAt], "id"), v[4]
			default:
				t.Fatal("unknown core oracle", kind)
			}
			if encode(got) != encode(want) {
				t.Fatalf("%s(%v): got %s, want %s", kind, v, encode(got), encode(want))
			}
		}
	}
}
