package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/claudeseal"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Codex calling a Claude model through uni-switch (mysterious_dove_96933 on
// Discord): Claude's thinking is signed, and a redacted_thinking block is
// sealed whole, and Anthropic wants both back intact on the next request
// of a tool loop. Codex keeps only what a reply's reasoning items carry,
// so they carry Claude's blocks in encrypted_content, and the next
// request has them back, in order, as Claude wrote them.

// claudeToolTurn is a Messages stream as Anthropic's extended thinking
// docs give one ending in a tool call: a signed thinking block, a
// redacted_thinking block, a block signed with no text (display omitted),
// then the call.
var claudeToolTurn = []string{
	`{"type":"message_start","message":{"id":"msg_01","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[],"usage":{"input_tokens":10,"output_tokens":1}}}`,
	`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"I should list "}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"the files."}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"ErUBCkYIBxgCIkB2signed"}}`,
	`{"type":"content_block_stop","index":0}`,
	`{"type":"content_block_start","index":1,"content_block":` + redactedBlock + `}`,
	`{"type":"content_block_stop","index":1}`,
	`{"type":"content_block_start","index":2,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
	`{"type":"content_block_delta","index":2,"delta":{"type":"signature_delta","signature":"EqQBCkYIBxgCKkDomitted"}}`,
	`{"type":"content_block_stop","index":2}`,
	`{"type":"content_block_start","index":3,"content_block":{"type":"tool_use","id":"toolu_01","name":"shell","input":{}}}`,
	`{"type":"content_block_delta","index":3,"delta":{"type":"input_json_delta","partial_json":"{\"command\":[\"ls\"]}"}}`,
	`{"type":"content_block_stop","index":3}`,
	`{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":30}}`,
	`{"type":"message_stop"}`,
}

// claudeTurnBlocks are the blocks claudeToolTurn's message is made of.
var claudeTurnBlocks = []string{
	`{"type":"thinking","thinking":"I should list the files.","signature":"ErUBCkYIBxgCIkB2signed"}`,
	redactedBlock,
	`{"type":"thinking","thinking":"","signature":"EqQBCkYIBxgCKkDomitted"}`,
	`{"type":"tool_use","id":"toolu_01","name":"shell","input":{"command":["ls"]}}`,
}

// claudeUp answers a request without a tool result with claudeToolTurn,
// and one with a result with a line of text, keeping what it was sent.
type claudeUp struct {
	mu   sync.Mutex
	sent []string
}

func (u *claudeUp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	u.mu.Lock()
	u.sent = append(u.sent, string(b))
	u.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	lines := claudeToolTurn
	if strings.Contains(string(b), `"tool_result"`) {
		lines = []string{
			`{"type":"message_start","message":{"id":"msg_02","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[],"usage":{"input_tokens":10,"output_tokens":1}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Two files."}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":3}}`,
			`{"type":"message_stop"}`,
		}
	}
	for _, l := range lines {
		var ev struct {
			Type string `json:"type"`
		}
		json.Unmarshal([]byte(l), &ev)
		io.WriteString(w, "event: "+ev.Type+"\ndata: "+l+"\n\n")
	}
}

// codexLoopAsk is a request as Codex sends one with store off: the reasoning
// items of a reply come back in input with their encrypted_content.
func codexLoopAsk(stream bool, input string) string {
	s := "false"
	if stream {
		s = "true"
	}
	return `{"model":"a/claude-sonnet-4-5","instructions":"You are a coding agent.","input":` + input + `,` +
		`"tools":[{"type":"function","name":"shell","description":"Runs a command","strict":false,"parameters":{"type":"object","properties":{"command":{"type":"array","items":{"type":"string"}}},"required":["command"]}}],` +
		`"tool_choice":"auto","parallel_tool_calls":false,"reasoning":{"effort":"medium","summary":"auto"},"store":false,"stream":` + s +
		`,"include":["reasoning.encrypted_content"],"prompt_cache_key":"t1"}`
}

// replyItems are a Responses reply's output items, from its
// response.output_item.done events when it streamed.
func replyItems(t *testing.T, body []byte, stream bool) []map[string]any {
	t.Helper()
	var items []map[string]any
	if !stream {
		var out struct {
			Output []map[string]any `json:"output"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("%v: %s", err, body)
		}
		return out.Output
	}
	for _, ev := range sseData(t, body) {
		if ev["type"] == "response.output_item.done" {
			items = append(items, ev["item"].(map[string]any))
		}
	}
	return items
}

// The tool loop as Codex runs it against a Claude model on an Anthropic
// API key: the second request reaches Anthropic with the first reply's
// signed thinking, redacted_thinking and text-less signed block, in
// order and as Claude wrote them, before the tool_use, streamed or not.
func TestCodexToolLoopGivesClaudeItsThinkingBack(t *testing.T) {
	for _, stream := range []bool{true, false} {
		fresh(t)
		up := &claudeUp{}
		srv := httptest.NewServer(up)
		t.Cleanup(srv.Close)
		if err := provider.Save(provider.Provider{ID: "a", Name: "A", Key: "k", Anthropic: srv.URL, Models: []string{"claude-sonnet-4-5"}}); err != nil {
			t.Fatal(err)
		}
		s := New()
		user := `{"type":"message","role":"user","content":[{"type":"input_text","text":"What is here?"}]}`
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(codexLoopAsk(stream, "["+user+"]"))))
		if rec.Code != 200 {
			t.Fatalf("stream=%t turn 1: %d %s", stream, rec.Code, rec.Body)
		}
		// Codex keeps each item as it came, less a reasoning item's id
		// (it doesn't send one back with store off), then the call's output
		input := []string{user}
		var call string
		for _, it := range replyItems(t, rec.Body.Bytes(), stream) {
			delete(it, "status")
			if it["type"] == "reasoning" {
				delete(it, "id")
			}
			if it["type"] == "function_call" {
				call, _ = it["call_id"].(string)
			}
			b, _ := json.Marshal(it)
			input = append(input, string(b))
		}
		if call == "" {
			t.Fatalf("stream=%t: no call in %s", stream, rec.Body)
		}
		input = append(input, `{"type":"function_call_output","call_id":"`+call+`","output":"a.go\nb.go"}`)
		rec = httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(codexLoopAsk(stream, "["+strings.Join(input, ",")+"]"))))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Two files.") {
			t.Fatalf("stream=%t turn 2: %d %s", stream, rec.Code, rec.Body)
		}
		up.mu.Lock()
		last := up.sent[len(up.sent)-1]
		up.mu.Unlock()
		var q struct {
			Messages []struct {
				Role    string            `json:"role"`
				Content []json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal([]byte(last), &q); err != nil {
			t.Fatal(err)
		}
		var got []json.RawMessage
		for _, m := range q.Messages {
			if m.Role == "assistant" {
				got = append(got, m.Content...)
			}
		}
		if len(got) != len(claudeTurnBlocks) {
			t.Fatalf("stream=%t: Anthropic was sent %d assistant blocks, want %d:\n%s", stream, len(got), len(claudeTurnBlocks), last)
		}
		for i, w := range claudeTurnBlocks {
			var g map[string]any
			json.Unmarshal(got[i], &g)
			delete(g, "cache_control")
			if !jsonIs(t, g, w) {
				t.Fatalf("stream=%t: block %d:\n got %s\nwant %s\nsent %s", stream, i, got[i], w, last)
			}
		}
	}
}

// claudeConversation is a Codex conversation that went through Claude and
// now goes on with another model: a reasoning item with a summary and
// Claude's block, one with Claude's block alone, and one OpenAI sealed.
func claudeConversation() string {
	think := claudeThinking("I should list the files.", "ErUBCkYIBxgCIkB2signed")
	redacted := claudeseal.Seal([]byte(redactedBlock))
	return `{"type":"message","role":"user","content":[{"type":"input_text","text":"What is here?"}]},` +
		`{"type":"reasoning","summary":[{"type":"summary_text","text":"I should list the files."}],"encrypted_content":"` + think + `"},` +
		`{"type":"reasoning","summary":[],"encrypted_content":"` + redacted + `"},` +
		`{"type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"command\":[\"ls\"]}"},` +
		`{"type":"function_call_output","call_id":"call_1","output":"a.go"},` +
		`{"type":"reasoning","id":"rs_openai","summary":[],"encrypted_content":"gAAAA-openai-sealed"},` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"and now?"}]}`
}

// Claude's blocks are Claude's: a Responses API (a relay here, the
// ChatGPT backend for Codex's own models) is sent each item as it was
// before uni-switch carried them, its summary alone or nothing, and never the
// envelope, which OpenAI would refuse as invalid_encrypted_content. What
// OpenAI sealed goes on untouched.
func TestClaudeBlocksNeverGoToAResponsesAPI(t *testing.T) {
	fresh(t)
	u := &relayUpstream{}
	saveRelay(t, u)
	body := `{"model":"relay/gpt-5.4","stream":true,"store":false,"include":["reasoning.encrypted_content"],"input":[` + claudeConversation() + `]}`
	if rec := codexPostTo(New(), body); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	asked := u.asked()
	if len(asked) != 1 {
		t.Fatalf("relay asked %d times", len(asked))
	}
	sent, _ := json.Marshal(asked[0])
	if strings.Contains(string(sent), claudeseal.Mark) || !strings.Contains(string(sent), "gAAAA-openai-sealed") {
		t.Fatalf("relay was sent %s", sent)
	}
	if !strings.Contains(string(sent), "I should list the files.") {
		t.Fatalf("the summary went missing: %s", sent)
	}

	// Codex's own model on the ChatGPT backend
	out, _ := codexInputAs([]byte(`{"model":"gpt-5.4","input":[`+claudeConversation()+`]}`), false, false)
	if strings.Contains(string(out), claudeseal.Mark) || !strings.Contains(string(out), "gAAAA-openai-sealed") {
		t.Fatalf("ChatGPT backend would be sent %s", out)
	}
	if strings.Count(string(out), `"reasoning"`) != 1 {
		t.Fatalf("want only OpenAI's reasoning item: %s", out)
	}

	// and nothing to take out, the body goes byte for byte
	plain := []byte(`{"input":[{"type":"reasoning","summary":[],"encrypted_content":"gAAAA"}],"x":1}`)
	if got := withoutClaudeSeals(plain); string(got) != string(plain) {
		t.Fatalf("%s became %s", plain, got)
	}
}

// Only Claude's signature goes in encrypted_content: a Gemini thought's
// (an unnamed KSig, or a Part with no SignedBy) isn't sealed as Claude's,
// streamed or not.
func TestGeminiSignatureIsNotSealedAsClaudes(t *testing.T) {
	rec := httptest.NewRecorder()
	enc := &responsesEncoder{w: newSSEWriter(rec), model: "m"}
	enc.event(Event{Kind: KThinkStart})
	enc.event(Event{Kind: KThink, Text: "thought"})
	enc.event(Event{Kind: KSig, Text: "gemini-sig"})
	enc.event(Event{Kind: KThinkStart})
	enc.event(Event{Kind: KSig, Text: "gemini-bare"})
	enc.event(Event{Kind: KText, Text: "hi"})
	enc.event(Event{Kind: KStop, Stop: "stop"})
	enc.finish()
	if strings.Contains(rec.Body.String(), "encrypted_content") || strings.Contains(rec.Body.String(), "gemini-") {
		t.Fatalf("stream: %s", rec.Body)
	}
	b := renderResponses(Result{Parts: []Part{{Kind: Thinking, Text: "thought", Signature: "gemini-sig"}, {Kind: Thinking, Signature: "gemini-bare"}, {Kind: Text, Text: "hi"}}}, "m", nil)
	if strings.Contains(string(b), "encrypted_content") || strings.Contains(string(b), "gemini-") {
		t.Fatalf("render: %s", b)
	}
}
