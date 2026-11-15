package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// codexUnverifiable is Codex's refusal of reasoning another vendor or
// account sealed, as #4798's reporters quote it (zane668, ZanderH-code).
const codexUnverifiable = "The encrypted content KqMXgAAAAABpGZWl could not be verified. Reason: Encrypted content could not be decrypted or parsed."

// carrySealer is a Responses upstream that seals its reasoning as
// "sealed-by-<name>#<its call>", each item its own, reads only its own, and answers each request it
// takes with that reasoning and a call, as Grok and Codex answer a tool
// loop. It keeps every request's input.
type carrySealer struct {
	name    string
	refusal string // its words for reasoning it can't read, as a 400
	mu      sync.Mutex
	down    int // answering this status, for the conversation to move on
	n       int
	inputs  [][]map[string]any
}

func (s *carrySealer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var q struct {
		Input []map[string]any `json:"input"`
	}
	json.Unmarshal(b, &q)
	foreign := false
	for _, it := range q.Input {
		if enc, _ := it["encrypted_content"].(string); it["type"] == "reasoning" && !strings.HasPrefix(enc, "sealed-by-"+s.name+"#") {
			foreign = true
		}
	}
	s.mu.Lock()
	s.inputs = append(s.inputs, q.Input)
	down := s.down
	s.n++
	n := s.n
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case down != 0:
		w.WriteHeader(down)
		io.WriteString(w, `{"error":{"message":"Grok Build usage balance exhausted","type":"insufficient_quota"}}`)
		return
	case foreign:
		w.WriteHeader(400)
		msg, _ := json.Marshal(s.refusal)
		io.WriteString(w, `{"error":{"message":`+string(msg)+`,"type":"invalid_request_error","param":null}}`)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	call := fmt.Sprintf("call_%s_%d", s.name, n)
	io.WriteString(w, sse(
		`data: {"type":"response.created","response":{"id":"r`+call+`","model":"m"}}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_`+call+`","summary":[],"encrypted_content":"sealed-by-`+s.name+`#`+call+`"}}`,
		`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_`+call+`","call_id":"`+call+`","name":"shell","arguments":"","status":"in_progress"}}`,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_`+call+`","output_index":1,"delta":"{\"cmd\":\"cat a.txt\"}"}`,
		`data: {"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"fc_`+call+`","call_id":"`+call+`","name":"shell","arguments":"{\"cmd\":\"cat a.txt\"}","status":"completed"}}`,
		`data: {"type":"response.completed","response":{"id":"r`+call+`","usage":{"input_tokens":7,"output_tokens":1}}}`))
}

// taken is what each request since the last call carried: its reasoning's
// sealer, in order, and the call ids and arguments of its calls and results.
func (s *carrySealer) taken() (seals [][]string, calls []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, in := range s.inputs {
		var got []string
		for _, it := range in {
			switch it["type"] {
			case "reasoning":
				by, _, _ := strings.Cut(strings.TrimPrefix(it["encrypted_content"].(string), "sealed-by-"), "#")
				got = append(got, by)
			case "function_call":
				calls = append(calls, it["call_id"].(string)+" "+it["arguments"].(string))
			case "function_call_output":
				calls = append(calls, it["call_id"].(string)+" -> "+fmt.Sprint(it["output"]))
			}
		}
		seals = append(seals, got)
	}
	s.inputs = nil
	return seals, calls
}

// carryClient is a client of one API in a tool loop: it keeps the calls it
// was given, ids as given, and sends them back with their results.
type carryClient struct {
	from   provider.Protocol
	stream bool
	calls  []string // the ids it was given, in order
}

func (c *carryClient) ask(t *testing.T, s *Server) (int, string) {
	t.Helper()
	const user = "Sum the numbers in a.txt"
	var path, body string
	st := fmt.Sprint(c.stream)
	switch c.from {
	case provider.Chat:
		msgs := `{"role":"user","content":"` + user + `"}`
		for _, id := range c.calls {
			msgs += `,{"role":"assistant","content":"Reading it.","tool_calls":[{"id":"` + id + `","type":"function","function":{"name":"shell","arguments":"{\"cmd\":\"cat a.txt\"}"}}]},{"role":"tool","tool_call_id":"` + id + `","content":"1 2 3"}`
		}
		path, body = "/v1/chat/completions", `{"model":"group/grok-codex","stream":`+st+`,"reasoning_effort":"high","tools":[{"type":"function","function":{"name":"shell","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}}],"messages":[`+msgs+`]}`
	case provider.Anthropic:
		msgs := `{"role":"user","content":"` + user + `"}`
		for _, id := range c.calls {
			msgs += `,{"role":"assistant","content":[{"type":"text","text":"Reading it."},{"type":"tool_use","id":"` + id + `","name":"shell","input":{"cmd":"cat a.txt"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"` + id + `","content":"1 2 3"}]}`
		}
		path, body = "/v1/messages", `{"model":"group/grok-codex","stream":`+st+`,"max_tokens":32000,"thinking":{"type":"adaptive"},"tools":[{"name":"shell","input_schema":{"type":"object","properties":{"cmd":{"type":"string"}}}}],"messages":[`+msgs+`]}`
	case provider.Gemini:
		msgs := `{"role":"user","parts":[{"text":"` + user + `"}]}`
		for _, id := range c.calls {
			msgs += `,{"role":"model","parts":[{"text":"Reading it."},{"functionCall":{"id":"` + id + `","name":"shell","args":{"cmd":"cat a.txt"}}}]},{"role":"user","parts":[{"functionResponse":{"id":"` + id + `","name":"shell","response":{"output":"1 2 3"}}}]}`
		}
		path, body = "/v1beta/models/group/grok-codex:generateContent", `{"tools":[{"functionDeclarations":[{"name":"shell","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}]}],"contents":[`+msgs+`]}`
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
	if rec.Code == 200 {
		c.calls = append(c.calls, c.givenID(t, rec.Body.Bytes()))
	}
	return rec.Code, rec.Body.String()
}

// givenID is the id of the one call the reply gave the client.
func (c *carryClient) givenID(t *testing.T, body []byte) string {
	t.Helper()
	if c.from != provider.Gemini {
		return askedCallID(t, c.from, body, c.stream)
	}
	var m struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					FunctionCall *struct{ ID string } `json:"functionCall"`
				}
			}
		}
	}
	json.Unmarshal(body, &m)
	var ids []string
	for _, cand := range m.Candidates {
		for _, p := range cand.Content.Parts {
			if p.FunctionCall != nil {
				ids = append(ids, p.FunctionCall.ID)
			}
		}
	}
	if len(ids) != 1 {
		t.Fatalf("gemini client was given calls %v: %s", ids, body)
	}
	return ids[0]
}
