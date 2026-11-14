package gateway

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/tidwall/gjson"
)

// anyUnphased, read in place, finds the items unphased finds in a decoded
// reply, so a reply it lets through as it came is one phaseOutput would
// have left alone.
func TestAnyUnphasedAgreesWithUnphased(t *testing.T) {
	items := []string{
		`{"type":"message","role":"assistant","phase":"commentary"}`,
		`{"type":"message","role":"assistant"}`,
		`{"type":"message"}`,
		`{"type":"message","role":"user"}`,
		`{"type":"message","role":null}`,
		`{"type":"message","role":"assistant","phase":""}`,
		`{"type":"message","role":"assistant","phase":null}`,
		`{"type":"message","role":"assistant","phase":1}`,
		`{"type":"reasoning"}`,
		`{"type":"function_call","name":"shell"}`,
		`{"type":["message"]}`,
		`"message"`,
	}
	for _, it := range items {
		var m map[string]any
		_ = json.Unmarshal([]byte(it), &m)
		want := unphased(m)
		if got := anyUnphased(gjson.Parse("[" + it + "]")); got != want {
			t.Errorf("%s: anyUnphased %v, unphased %v", it, got, want)
		}
		two := `[{"type":"message","role":"assistant","phase":"final_answer"},` + it + `]`
		if got := anyUnphased(gjson.Parse(two)); got != want {
			t.Errorf("after a phased message, %s: anyUnphased %v, unphased %v", it, got, want)
		}
	}
	if anyUnphased(gjson.Parse(`{"type":"message"}`)) {
		t.Error("an output that isn't a list has nothing to phase")
	}
}

func TestEventDataJoinsAsSSE(t *testing.T) {
	for ev, want := range map[string]string{
		"event: x\ndata: {\"a\":1}\n\n":    `{"a":1}`,
		"data:{\"a\":1}\r\n\r\n":           `{"a":1}`,
		"data: a\ndata:  b\ndata: c\n\n":   "a\n b\nc",
		": ping\n\n":                       "",
		"id: 1\ndata: one\r\nretry: 5\n\n": "one",
	} {
		if got := string(eventData([]byte(ev))); got != want {
			t.Errorf("%q: %q, want %q", ev, got, want)
		}
	}
}

// A stream split anywhere comes out of both tidies as it went in when
// nothing in it needs tidying, however its writes fall.
func TestTidiesPassAStreamSplitAnywhere(t *testing.T) {
	stream := "event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"m1\",\"type\":\"message\"}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"m1\",\"delta\":\"hi\"}\r\n\r\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"m1\",\"type\":\"message\",\"role\":\"assistant\",\"phase\":\"final_answer\"}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"output\":[{\"id\":\"m1\",\"type\":\"message\",\"role\":\"assistant\",\"phase\":\"final_answer\"}]}}\n\n"
	for _, step := range []int{1, 7, 64, len(stream)} {
		ids, phases := &idTidy{}, &phaseTidy{sse: true}
		var got []byte
		for i := 0; i < len(stream); i += step {
			b := []byte(stream[i:min(i+step, len(stream))])
			got = append(got, phases.write(ids.write(b))...)
			clear(b) // the caller's buffer is read again for its next write
		}
		got = append(got, phases.write(ids.flush())...)
		got = append(got, phases.flush()...)
		if !bytes.Equal(got, []byte(stream)) {
			t.Fatalf("written %d bytes at a time:\n%s\nwant\n%s", step, got, stream)
		}
	}
}
