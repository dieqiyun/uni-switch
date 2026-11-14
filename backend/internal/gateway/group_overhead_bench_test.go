package gateway

import (
	"bytes"
	"fmt"
	"io"

	"net/http"
)

// groupBenchUpstream answers an Anthropic request as it asked: streamed
// when its body says so. Refusing, it answers code with an overloaded
// error.
type groupBenchUpstream struct{ code int }

const groupBenchReply = `{"id":"msg_1","type":"message","role":"assistant","model":"m1","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":7,"output_tokens":1}}`

var groupBenchStream = sse(
	`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"msg_1","model":"m1","usage":{"input_tokens":7}}}`,
	`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
	`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":0}`,
	`event: message_delta`+"\n"+`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
	`event: message_stop`+"\n"+`data: {"type":"message_stop"}`)

func (u groupBenchUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	if u.code != 0 {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(u.code)
		io.WriteString(w, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
		return
	}
	if bytes.Contains(b, []byte(`"stream":true`)) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, groupBenchStream)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, groupBenchReply)
}

// groupBenchCase is one benchmark's request.
type groupBenchCase struct {
	providers, models, words int
	stream                   bool
	model                    string
}

func (c groupBenchCase) name() string {
	return fmt.Sprintf("catalog=%dx%d/%dKB/stream=%v/%s", c.providers, c.models, c.words*4/1000, c.stream, c.model)
}

// groupBenchCases: a dozen providers of 20 models, with a ~200KB and a
// ~2MB conversation, and a large catalog (30 providers of 60 models, as
// a user of several relays has) with the ~200KB one.
func groupBenchCases() []groupBenchCase {
	var out []groupBenchCase
	for _, size := range []struct{ providers, models, words int }{{12, 20, 50_000}, {12, 20, 500_000}, {30, 60, 50_000}} {
		for _, stream := range []bool{false, true} {
			for _, model := range []string{"p0/m1", "group/g1", "group/g4", "group/g8u", "group/gfail", "group/grest"} {
				out = append(out, groupBenchCase{size.providers, size.models, size.words, stream, model})
			}
		}
	}
	return out
}
