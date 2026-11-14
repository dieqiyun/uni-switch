package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// An agent that hangs up the moment a stream's last event reaches it had the
// whole reply, however the upstream spelled that event: SSE makes the space
// after "data:" or "event:" optional, and relays leave it out (#1978).
func TestHangUpAfterStreamEndIsNotCanceled(t *testing.T) {
	// the tail of the reporter's relay stream, as curl printed it
	chat := `data: {"id":"c1","object":"chat.completion.chunk","model":"deepseek-v4.1-flash","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_time","arguments":"{}"}}]},"finish_reason":null}]}` + "\n\n" +
		`data: {"id":"c1","object":"chat.completion.chunk","model":"deepseek-v4.1-flash","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":120,"completion_tokens":31,"total_tokens":151,"completion_tokens_details":{"reasoning_tokens":0}}}` + "\n\n"
	anthropic := func(sep string) string {
		return "event:" + sep + "message_start\ndata:" + sep + `{"type":"message_start","message":{"id":"m1","type":"message","role":"assistant","model":"claude-x","content":[],"usage":{"input_tokens":120,"output_tokens":1}}}` + "\n\n" +
			"event:" + sep + "content_block_start\ndata:" + sep + `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n" +
			"event:" + sep + "content_block_delta\ndata:" + sep + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}` + "\n\n" +
			"event:" + sep + "content_block_stop\ndata:" + sep + `{"type":"content_block_stop","index":0}` + "\n\n" +
			"event:" + sep + "message_delta\ndata:" + sep + `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":31}}` + "\n\n" +
			"event:" + sep + "message_stop\ndata:" + sep + `{"type": "message_stop"}` + "\n\n"
	}
	for _, c := range []struct {
		name, path, body, stream string
	}{
		{"chat spaced", "/v1/chat/completions", `{"model":"relay/m","stream":true,"messages":[{"role":"user","content":"Call the get_time tool."}]}`, chat + "data: [DONE]\n\n"},
		{"chat unspaced", "/v1/chat/completions", `{"model":"relay/m","stream":true,"messages":[{"role":"user","content":"Call the get_time tool."}]}`, chat + "data:[DONE]\n\n"},
		{"chat unspaced CRLF", "/v1/chat/completions", `{"model":"relay/m","stream":true,"messages":[{"role":"user","content":"Call the get_time tool."}]}`, strings.ReplaceAll(chat, "\n", "\r\n") + "data:[DONE]\r\n\r\n"},
		{"anthropic unspaced", "/v1/messages", `{"model":"relay/m","stream":true,"max_tokens":200,"messages":[{"role":"user","content":"hi"}]}`, anthropic("")},
	} {
		t.Run(c.name, func(t *testing.T) {
			fresh(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				io.WriteString(w, c.stream)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer up.Close()
			p := provider.Provider{ID: "relay", Name: "Relay", Key: "test", Models: []string{"m"}}
			if strings.HasPrefix(c.path, "/v1/messages") {
				p.Anthropic = up.URL
			} else {
				p.Chat = up.URL + "/v1"
			}
			if err := provider.Save(p); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", c.path, strings.NewReader(c.body)).WithContext(ctx)
			// the agent hangs up the moment its last event arrives, before
			// the try is over
			agent := &hangUpAt{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
			New().Handler().ServeHTTP(agent, req)
			if !agent.hungUp {
				t.Fatalf("the stream's end never reached the agent:\n%s", agent.Body.String())
			}
			records := usage.Load(time.Time{})
			if len(records) != 1 {
				t.Fatalf("ledger has %d records, want 1", len(records))
			}
			if r := records[0]; r.Status != http.StatusOK || r.Output != 31 {
				t.Fatalf("a reply the agent had whole was logged as %d %q, out=%d", r.Status, r.Error, r.Output)
			}
		})
	}
}

// hangUpAt is an agent that hangs up once the stream's last event has
// reached it, as the OpenAI SDK does on [DONE].
type hangUpAt struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
	hungUp bool
}

func (h *hangUpAt) Write(b []byte) (int, error) {
	n, err := h.ResponseRecorder.Write(b)
	if s := h.Body.String(); strings.Contains(s, "[DONE]") || strings.Contains(s, "message_stop\"}") {
		h.hungUp = true
		h.cancel()
	}
	return n, err
}

// A stream's end is seen in each spelling, split across two writes too, and
// never in a reply's text that only talks about one.
func TestHoldWriterSeesEveryStreamEnd(t *testing.T) {
	chunk := `data: {"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}` + "\n\n"
	for _, c := range []struct {
		name, stream string
		ended        bool
	}{
		{"chat spaced", chunk + "data: [DONE]\n\n", true},
		{"chat unspaced", chunk + "data:[DONE]\n\n", true},
		{"chat unspaced CRLF", chunk + "data:[DONE]\r\n\r\n", true},
		{"anthropic event unspaced", "event:message_stop\ndata:{}\n\n", true},
		{"anthropic JSON spaced", "data: {\"type\": \"message_stop\"}\n\n", true},
		{"responses JSON spaced", "data: {\"type\": \"response.completed\",\"response\":{}}\n\n", true},
		{"responses incomplete spaced", "data: {\"type\": \"response.incomplete\",\"response\":{}}\n\n", true},
		{"text about DONE", `data: {"choices":[{"index":0,"delta":{"content":"a stream ends with data:[DONE] or data: [DONE]"},"finish_reason":null}]}` + "\n\n", false},
		{"text about message_stop", `data: {"choices":[{"index":0,"delta":{"content":"then event:message_stop and {\"type\": \"message_stop\"}"},"finish_reason":null}]}` + "\n\n", false},
	} {
		for cut := 1; cut < len(c.stream); cut++ {
			h := &holdWriter{}
			h.see([]byte(c.stream[:cut]))
			h.see([]byte(c.stream[cut:]))
			if h.ended != c.ended {
				t.Fatalf("%s cut at %d: ended = %v, want %v", c.name, cut, h.ended, c.ended)
			}
		}
	}
}
