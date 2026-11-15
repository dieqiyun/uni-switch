package sessions

import (
	"bytes"
	"strings"
	"testing"
)

// cxMarksByContains is what callHead and codexCallLine found with a
// bytes.Contains of each value, a pass over the line each.
func cxMarksByContains(b []byte) (m cxMarks) {
	for v, mark := range map[string]cxMarks{
		`"type":"user_message"`: cxMarkUserMsg, `"role":"user"`: cxMarkUserRole,
		`"type":"function_call_output"`: cxMarkToolOut, `"type":"custom_tool_call_output"`: cxMarkToolOut,
		`"type":"session_meta"`: cxMarkMeta, `"type":"turn_context"`: cxMarkTurn,
		`"type":"token_usage_record"`: cxMarkRecord, `"type":"compacted"`: cxMarkCompacted,
		`"type":"token_count"`: cxMarkCount, `"type":"thread_settings_applied"`: cxMarkSettings,
		`"type":"task_started"`: cxMarkStarted, `"type":"task_complete"`: cxMarkDone,
	} {
		if bytes.Contains(b, []byte(v)) {
			m |= mark
		}
	}
	return m
}

// One pass finds what each value's own pass found, wherever it is in the
// line: the head, nested past a long string, a string's escaped copy (not
// found by either), a key whose name only ends in "type".
func TestCxMarksAsContains(t *testing.T) {
	long := strings.Repeat(`x\":\"y \"type\":\"token_count\" `, 2000)
	lines := []string{
		`{"timestamp":"2026-10-25T08:00:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1}}}}`,
		`{"timestamp":"2026-10-25T08:00:00.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}}`,
		`{"timestamp":"2026-10-25T08:00:00.000Z","type":"response_item","payload":{"type":"function_call_output","call_id":"c","output":"` + long + `"}}`,
		`{"timestamp":"2026-10-25T08:00:00.000Z","type":"event_msg","payload":{"type":"exec_command_end","aggregated_output":"` + long + `"}}`,
		`{"timestamp":"2026-10-25T08:00:00.000Z","type":"event_msg","payload":{"type":"agent_message","text":"` + long + `","inner":{"type":"task_complete"}}}`,
		`{"timestamp":"2026-10-25T08:00:00.000Z","type":"compacted","payload":{"replacement_history":[{"type":"message","role":"user"},{"type":"message","role":"assistant"}]}}`,
		`{"timestamp":"2026-10-25T08:00:00.000Z","type":"session_meta","payload":{"id":"s","base_instructions":{"text":"` + long + `"}}}`,
		`{"timestamp":"2026-10-25T08:00:00.000Z","type":"turn_context","payload":{"model":"gpt-5.5","subtype":"user_message","xtype":"task_started"}}`,
		`{"type":"token_usage_record","payload":{"type":"thread_settings_applied","role":"users","r":{"role":"user"}}}`,
		`{"a":":","type":"custom_tool_call_output","b":"\"role\":\"user\""}`,
		`"type":"task_started"`, `"type":"`, `"role":"`, `":"`, ``, `{"type":"user_message`,
	}
	for _, l := range lines {
		b := []byte(l)
		if got, want := cxMarksOf(b), cxMarksByContains(b); got != want {
			t.Errorf("cxMarksOf(%.120q) = %b, the passes of each found %b", l, got, want)
		}
	}
}

// A line that names none of the values is passed over once: no more than
// a bytes.Index of `":"` would cost, not one pass a value.
func BenchmarkCxMarks(b *testing.B) {
	line := []byte(`{"timestamp":"2026-10-25T08:00:00.000Z","type":"event_msg","payload":{"type":"exec_command_end","aggregated_output":"` + strings.Repeat(`line of output \"quoted\" `, 40000) + `"}}`)
	b.SetBytes(int64(len(line)))
	b.Run("one pass", func(b *testing.B) {
		for range b.N {
			cxMarksOf(line)
		}
	})
	b.Run("a pass each", func(b *testing.B) {
		for range b.N {
			cxMarksByContains(line)
		}
	})
}
