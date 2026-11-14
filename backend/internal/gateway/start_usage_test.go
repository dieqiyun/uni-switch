package gateway

import (
	"bufio"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// toAnthropic runs a stream's chunks through a decoder and the Anthropic
// encoder: what Claude Code is answered.
func toAnthropic(t *testing.T, decode func(string, func(Event)) error, chunks ...string) []map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	enc := &anthropicEncoder{w: newSSEWriter(rec), model: "m"}
	for _, c := range chunks {
		if err := decode(c, enc.event); err != nil {
			t.Fatal(err)
		}
	}
	enc.finish()
	var out []map[string]any
	sc := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	for sc.Scan() {
		if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			var m map[string]any
			if json.Unmarshal([]byte(data), &m) == nil {
				out = append(out, m)
			}
		}
	}
	return out
}

// usageOfType is the usage the stream's first event of that type told.
func usageOfType(t *testing.T, out []map[string]any, typ string) map[string]any {
	t.Helper()
	for _, ev := range out {
		if ev["type"] != typ {
			continue
		}
		if typ == "message_start" {
			return ev["message"].(map[string]any)["usage"].(map[string]any)
		}
		return ev["usage"].(map[string]any)
	}
	t.Fatalf("no %s in %v", typ, out)
	return nil
}

// A stream that counts the prompt on its first chunk tells it on
// message_start, which Claude Code's context meter reads while the turn
// is still being written (#1887); message_delta still tells the whole
// turn, the prompt counted once.
func TestPromptUsageOnMessageStart(t *testing.T) {
	for _, c := range []struct {
		name   string
		decode func(string, func(Event)) error
		chunks []string
		start  map[string]float64
		end    map[string]float64
	}{
		{
			name:   "gemini",
			decode: (&codeAssistDecoder{}).decode,
			chunks: []string{
				`{"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"Read"}]}}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":1,"cachedContentTokenCount":40}}}`,
				`{"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"ing."}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":10,"cachedContentTokenCount":40}}}`,
			},
			start: map[string]float64{"input_tokens": 60, "cache_read_input_tokens": 40, "output_tokens": 0},
			end:   map[string]float64{"input_tokens": 60, "cache_read_input_tokens": 40, "output_tokens": 10},
		},
		{
			name:   "chat counting on every chunk",
			decode: (&chatDecoder{}).decode,
			chunks: []string{
				`{"id":"x","choices":[{"index":0,"delta":{"content":"Read"}}],"usage":{"prompt_tokens":100,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":40}}}`,
				`{"id":"x","choices":[{"index":0,"delta":{"content":"ing."},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":40}}}`,
			},
			start: map[string]float64{"input_tokens": 60, "cache_read_input_tokens": 40, "output_tokens": 0},
			end:   map[string]float64{"input_tokens": 60, "cache_read_input_tokens": 40, "output_tokens": 10},
		},
		{
			// most chat streams count only at their end: nothing to tell
			// at the start
			name:   "chat counting at the end",
			decode: (&chatDecoder{}).decode,
			chunks: []string{
				`{"id":"x","choices":[{"index":0,"delta":{"content":"Reading."}}]}`,
				`{"id":"x","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
				`{"id":"x","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":40}}}`,
			},
			start: map[string]float64{"input_tokens": 0, "cache_read_input_tokens": 0, "output_tokens": 0},
			end:   map[string]float64{"input_tokens": 60, "cache_read_input_tokens": 40, "output_tokens": 10},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := toAnthropic(t, c.decode, c.chunks...)
			for typ, want := range map[string]map[string]float64{"message_start": c.start, "message_delta": c.end} {
				u := usageOfType(t, out, typ)
				for k, v := range want {
					if u[k] != v {
						t.Errorf("%s usage = %v, want %v", typ, u, want)
						break
					}
				}
			}
		})
	}
}
