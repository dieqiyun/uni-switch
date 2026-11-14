package gateway

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// slowThinker streams its reasoning a piece at a time, as GLM on ZCode's
// Start Plan does, and notes when it has said its last.
type slowThinker struct {
	events []string
	gap    time.Duration
	done   chan time.Time
	sent   []time.Time // when each event was written; read after done
}

func (s *slowThinker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.ReadAll(r.Body)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	f := w.(http.Flusher)
	for _, ev := range s.events {
		io.WriteString(w, ev+"\n\n")
		f.Flush()
		s.sent = append(s.sent, time.Now())
		time.Sleep(s.gap)
	}
	// a try that is asked again serves the same fake vendor a second
	// time; the channel holds one send, so a second handler would block
	// here forever and hold the reply it serves open, the package hung
	// on the server's Close with it until go test's own timeout (#4530,
	// Jackzigen). The tests read the time once; later sends can drop.
	select {
	case s.done <- time.Now():
	default:
	}
}

var glmThinksSlowly = []string{
	`event: message_start` + "\n" + `data: {"type":"message_start","message":{"id":"m1","role":"assistant","model":"glm-5.1","content":[]}}`,
	`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
	`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"first thought"}}`,
	`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":" second thought"}}`,
	`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":" third thought"}}`,
	`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":0}`,
	`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
	`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"done"}}`,
	`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":1}`,
	`event: message_delta` + "\n" + `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":9}}`,
	`event: message_stop` + "\n" + `data: {"type":"message_stop"}`,
}

var glmChatThinksSlowly = []string{
	`data: {"id":"c1","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"first thought"}}]}`,
	`data: {"id":"c1","choices":[{"index":0,"delta":{"reasoning_content":" second thought"}}]}`,
	`data: {"id":"c1","choices":[{"index":0,"delta":{"reasoning_content":" third thought"}}]}`,
	`data: {"id":"c1","choices":[{"index":0,"delta":{"content":"done"}}]}`,
	`data: {"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	`data: [DONE]`,
}

// Claude's reasoning is still held for a refusal after it (#248) while
// another candidate could answer, and so are OpenAI's, Gemini's and
// DeepSeek's (plugins #540); anybody else's is let through.
func TestThinkingHeldFor(t *testing.T) {
	for model, held := range map[string]bool{
		"claude-opus-5-5": true, "anthropic/claude-sonnet-4.5": true, "gpt-5.2-codex": true, "o3": true,
		"gemini-3-pro-preview": true, "models/gemini-2.5-flash": true,
		"deepseek-v4": true, "deepseek-v4.1-flash": true, "deepseek/deepseek-v4-pro": true,
		"GLM-5.1": false, "glm-4.6": false, "kimi-k2.5": false, "MiniMax-M2": false,
	} {
		if got := refusesAfterThinking(model); got != held {
			t.Errorf("%s: held %v, want %v", model, got, held)
		}
	}
}

// A DeepSeek chat reply that reasoned and went on to say it, and one whose
// filter ended it with nothing said (#540's shape), both as #4512's member
// serves them.
var deepseekThoughtThenAnswered = sse(
	`data: {"id":"c1","object":"chat.completion.chunk","model":"deepseek-v4.1-flash","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"The user is asking"},"finish_reason":null}]}`,
	`data: {"id":"c1","object":"chat.completion.chunk","model":"deepseek-v4.1-flash","choices":[{"index":0,"delta":{"reasoning_content":" about it."},"finish_reason":null}]}`,
	`data: {"id":"c1","object":"chat.completion.chunk","model":"deepseek-v4.1-flash","choices":[{"index":0,"delta":{"content":"from a"},"finish_reason":null}]}`,
	`data: {"id":"c1","object":"chat.completion.chunk","model":"deepseek-v4.1-flash","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`,
	`data: [DONE]`)

var deepseekThoughtThenRefused = sse(
	`data: {"id":"c1","object":"chat.completion.chunk","model":"deepseek-v4.1-flash","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"The user is asking"},"finish_reason":null}]}`,
	`data: {"id":"c1","object":"chat.completion.chunk","model":"deepseek-v4.1-flash","choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`,
	`data: [DONE]`)

var deepseekChatThinksSlowly = []string{
	`data: {"id":"c1","object":"chat.completion.chunk","model":"deepseek-v4.1-flash","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"first thought"},"finish_reason":null}]}`,
	`data: {"id":"c1","object":"chat.completion.chunk","model":"deepseek-v4.1-flash","choices":[{"index":0,"delta":{"reasoning_content":" second thought"},"finish_reason":null}]}`,
	`data: {"id":"c1","object":"chat.completion.chunk","model":"deepseek-v4.1-flash","choices":[{"index":0,"delta":{"reasoning_content":" third thought"},"finish_reason":null}]}`,
	`data: {"id":"c1","object":"chat.completion.chunk","model":"deepseek-v4.1-flash","choices":[{"index":0,"delta":{"content":"done"},"finish_reason":null}]}`,
	`data: {"id":"c1","object":"chat.completion.chunk","model":"deepseek-v4.1-flash","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`,
	`data: [DONE]`,
}

// A try that is asked again asks the same fake vendor again: a reply
// that only reasoned is asked again in place (#667), and a full run's
// state may have any ask retried. The fake's done holds one send, and
// the ask's first serve has filled it: a second handler that blocks on
// the send never returns, the reply it serves never ends, and the
// package hangs on vendor's Close until go test's own timeout (#4530).
// Asked again, the fake must serve to its end regardless.
func TestThinkingVendorAskedAgainStillEnds(t *testing.T) {
	fresh(t)
	thinks := []string{
		`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"first thought","thought":true}]}}],"modelVersion":"gemini-3-pro"}`,
		`data: {"candidates":[{"content":{"role":"model","parts":[{"text":" second thought","thought":true}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"thoughtsTokenCount":8},"modelVersion":"gemini-3-pro"}`,
	}
	up := &slowThinker{events: thinks, gap: 0, done: make(chan time.Time, 1)}
	vendor := httptest.NewServer(up)
	t.Cleanup(vendor.Close)
	p := provider.Provider{ID: "relay", Name: "Relay", Key: "k", Models: []string{"gemini-3-pro"}}
	p.Gemini = vendor.URL + "/v1beta"
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(New().Handler())
	t.Cleanup(gw.Close)

	res, err := http.Post(gw.URL+"/v1/messages", "application/json",
		strings.NewReader(strings.Replace(claudeCodeAsk, "group/g", "relay/gemini-3-pro", 1)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	// the reply that said nothing is asked again in place: read it to its
	// end, whichever try ends it — a handler stuck on done never ends it
	rd := bufio.NewReader(res.Body)
	for {
		if _, err := rd.ReadString('\n'); err != nil {
			break
		}
	}
	select {
	case <-up.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the vendor never finished")
	}
	// the ask-again came: the second serve went to its end, its events
	// noted after the first serve's
	if len(up.sent) < 2*len(thinks) {
		t.Fatalf("the vendor served %d events of %d asks, the ask-again never ended", len(up.sent), 2)
	}
}

// Claude's or OpenAI's reasoning at a provider nothing else stands behind
// is streamed as it comes: held for a refusal (#248) that would go to the
// agent anyway, as the last candidate's does, a relay's Claude showed
// Claude Code its thinking all at once with the text, and Codex none until
// then (iTianbao on X). Each of the relay's APIs, to Claude Code and
// Codex. Gemini's too, which a reply that only reasoned no longer needs
// held for (it is asked again in place, #667): held, Codex on a Gemini
// model had nothing, not even response.created, until the text began
// (LuckymanJC on Discord).
func TestThinkingStreamsWithNoOneElse(t *testing.T) {
	geminiThinks := []string{
		`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"first thought","thought":true}]}}],"modelVersion":"gemini-3-pro"}`,
		`data: {"candidates":[{"content":{"role":"model","parts":[{"text":" second thought","thought":true}]}}],"modelVersion":"gemini-3-pro"}`,
		`data: {"candidates":[{"content":{"role":"model","parts":[{"text":" third thought","thought":true}]}}],"modelVersion":"gemini-3-pro"}`,
		`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"done"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"thoughtsTokenCount":8},"modelVersion":"gemini-3-pro"}`,
	}
	responsesThinks := []string{
		`event: response.created` + "\n" + `data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress","output":[]}}`,
		`event: response.output_item.added` + "\n" + `data: {"type":"response.output_item.added","output_index":0,"item":{"id":"rs_1","type":"reasoning","summary":[]}}`,
		`event: response.reasoning_summary_part.added` + "\n" + `data: {"type":"response.reasoning_summary_part.added","item_id":"rs_1","output_index":0,"summary_index":0,"part":{"type":"summary_text","text":""}}`,
		`event: response.reasoning_summary_text.delta` + "\n" + `data: {"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":0,"summary_index":0,"delta":"first thought"}`,
		`event: response.reasoning_summary_text.delta` + "\n" + `data: {"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":0,"summary_index":0,"delta":" second thought"}`,
		`event: response.reasoning_summary_text.delta` + "\n" + `data: {"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":0,"summary_index":0,"delta":" third thought"}`,
		`event: response.output_item.done` + "\n" + `data: {"type":"response.output_item.done","output_index":0,"item":{"id":"rs_1","type":"reasoning","summary":[{"type":"summary_text","text":"first thought second thought third thought"}]}}`,
		`event: response.output_text.delta` + "\n" + `data: {"type":"response.output_text.delta","item_id":"m1","output_index":1,"content_index":0,"delta":"done"}`,
		`event: response.completed` + "\n" + `data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"id":"rs_1","type":"reasoning","summary":[{"type":"summary_text","text":"first thought second thought third thought"}]},{"id":"m1","type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}],"usage":{"input_tokens":1,"output_tokens":9,"total_tokens":10}}}`,
	}
	codexAsks := `{"model":"relay/MODEL","stream":true,"reasoning":{"effort":"medium","summary":"auto"},"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`
	claudeAsks := strings.Replace(claudeCodeAsk, "group/g", "relay/MODEL", 1)
	for _, x := range []struct {
		name   string
		proto  provider.Protocol
		events []string
		model  string
	}{
		{"anthropic", provider.Anthropic, glmThinksSlowly, "claude-opus-5-5"},
		{"chat", provider.Chat, glmChatThinksSlowly, "claude-opus-5-5"},
		{"chat gpt", provider.Chat, glmChatThinksSlowly, "gpt-5.2"},
		{"responses", provider.Responses, responsesThinks, "gpt-5.2"},
		{"gemini", provider.Gemini, geminiThinks, "gemini-3-pro"},
		{"chat gemini", provider.Chat, glmChatThinksSlowly, "gemini-3-pro"},
	} {
		for _, ask := range []struct{ path, body, thought, text string }{
			{"/v1/messages", claudeAsks, `"thinking_delta"`, `"text_delta"`},
			{"/v1/responses", codexAsks, `"response.reasoning_summary_text.delta"`, `"response.output_text.delta"`},
		} {
			t.Run(x.name+ask.path, func(t *testing.T) {
				fresh(t)
				up := &slowThinker{events: x.events, gap: 200 * time.Millisecond, done: make(chan time.Time, 1)}
				vendor := httptest.NewServer(up)
				t.Cleanup(vendor.Close)
				p := provider.Provider{ID: "relay", Name: "Relay", Key: "k", Models: []string{x.model}}
				switch x.proto {
				case provider.Anthropic:
					p.Anthropic = vendor.URL
				case provider.Chat:
					p.Chat = vendor.URL + "/v1"
				case provider.Gemini:
					p.Gemini = vendor.URL + "/v1beta"
				default:
					p.Responses = vendor.URL + "/v1"
				}
				if err := provider.Save(p); err != nil {
					t.Fatal(err)
				}
				gw := httptest.NewServer(New().Handler())
				t.Cleanup(gw.Close)
				res, err := http.Post(gw.URL+ask.path, "application/json", strings.NewReader(strings.Replace(ask.body, "MODEL", x.model, 1)))
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				var firstThought, firstText time.Time
				rd := bufio.NewReader(res.Body)
				for {
					ln, err := rd.ReadString('\n')
					if firstThought.IsZero() && strings.Contains(ln, ask.thought) {
						firstThought = time.Now()
					}
					if firstText.IsZero() && strings.Contains(ln, ask.text) {
						firstText = time.Now()
					}
					if err != nil {
						break
					}
				}
				select {
				case <-up.done:
				case <-time.After(5 * time.Second):
					t.Fatal("the vendor never finished")
				}
				if firstThought.IsZero() || firstText.IsZero() {
					t.Fatal("no thinking or no text reached the client")
				}
				if firstText.Sub(firstThought) < 300*time.Millisecond {
					t.Fatalf("thinking held: first thought only %v before the text", firstText.Sub(firstThought))
				}
			})
		}
	}
}
