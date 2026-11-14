package gateway

import (
	"encoding/json"

	"net/http/httptest"
	"regexp"
	"strings"

	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A byte-size refusal alone is not evidence of a token-window overflow.
// An independent text estimate well past the window still asks for compaction.
func TestPayloadTooLargeIsNotContextOverflow(t *testing.T) {
	for _, msg := range []string{
		"Factory: Request Entity Too Large",
		`{"error":{"type":"request_too_large","message":"Request exceeds the maximum size"}}`,
	} {
		if tooLong(413, msg) || overflowed(413, []byte(msg)) {
			t.Errorf("byte limit classified as token limit: %s", msg)
		}
		p := provider.Provider{ID: "fake", Name: "Fake", Contexts: map[string]int{"m": 100}}
		// Image bytes don't make a text estimate pass the token margin.
		tokens := estimate(&Request{Messages: []Message{{Role: "user", Parts: []Part{
			{Kind: Text, Text: strings.Repeat("word", 100)},
			{Kind: Image, Data: strings.Repeat(pngA, 4096)},
		}}}})
		if inferred, ok := tooLongUnsaid(p, "m", tokens, 0, 413, []byte(msg)); ok {
			t.Errorf("byte limit inferred as token limit: %s", inferred)
		}
		// a 413 is read as an overflow only for Factory (#711), whose
		// plugin serves it; any other vendor's byte cap is its own
		if inferred, ok := tooLongUnsaid(p, "m", 1000, 0, 413, []byte(msg)); ok {
			t.Errorf("another vendor's byte cap inferred as a token limit: %s", inferred)
		}
		factory := provider.PluginForTest("factory", "factory")
		factory.Name, factory.Contexts = "Factory", map[string]int{"m": 100}
		if inferred, ok := tooLongUnsaid(factory, "m", 1000, 0, 413, []byte(msg)); !ok || !strings.Contains(inferred, "prompt is too long") {
			t.Errorf("text estimate past the window lost compaction: %s", inferred)
		}
		if retryable(413, []byte(msg)) {
			t.Error("unchanged oversized request would be retried")
		}
	}
}

func payloadAnswerStream() string {
	return sse(
		`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"ok","role":"assistant","content":[],"usage":{"input_tokens":1}}}`,
		`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
		`event: message_stop`+"\n"+`data: {"type":"message_stop"}`)
}

func TestPayloadTooLargeErrorShapes(t *testing.T) {
	for _, proto := range []provider.Protocol{provider.Responses, provider.Chat, provider.Anthropic, provider.Gemini} {
		t.Run(string(proto), func(t *testing.T) {
			msg := "Factory: request_too_large: Request exceeds the maximum size"
			held := newHoldWriter(httptest.NewRecorder(), true)
			writeError(held, proto, 413, msg)
			if strings.Contains(string(held.errBody()), "byte limit") {
				t.Fatal("byte hint added before other members were tried")
			}
			immediate := httptest.NewRecorder()
			writeError(newHoldWriter(immediate, false), proto, 413, msg)
			if strings.Count(immediate.Body.String(), "byte limit") != 1 {
				t.Fatal("final unheld byte refusal lost its hint")
			}
			for range 2 { // another uni-switch can relay a message already carrying the hint
				w := httptest.NewRecorder()
				status := writeError(w, proto, 413, msg)
				var body struct {
					Error struct {
						Type    string `json:"type"`
						Message string `json:"message"`
						Code    any    `json:"code"`
					} `json:"error"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if status != 413 || w.Code != 413 || body.Error.Code == "context_length_exceeded" || strings.Count(body.Error.Message, "byte limit") != 1 {
					t.Fatalf("byte refusal mapped to context overflow: %d %s", status, w.Body.String())
				}
				if proto == provider.Anthropic && body.Error.Type != "request_too_large" {
					t.Fatalf("wrong Anthropic error type: %s", w.Body.String())
				}
				msg = body.Error.Message
			}
		})
	}
}

// A vendor's byte cap reaches an agent on the OpenAI routes in words pi
// knows (its overflow.ts matches request_too_large), so pi compacts and
// tries once more instead of stopping — @wilsonxuuuu on X, whose pi got
// an api_error 413 and never compacted.
func TestPayloadTooLargeSaysRequestTooLargeToPi(t *testing.T) {
	const nginx = "<html>\r\n<head><title>413 Request Entity Too Large</title></head>\r\n<body>\r\n<center><h1>413 Request Entity Too Large</h1></center>\r\n<hr><center>nginx</center>\r\n</body>\r\n</html>\r\n"
	piOverflow := regexp.MustCompile(`(?i)request_too_large`)
	fresh(t)
	scriptedOn(t, "a", provider.Chat, &scripted{replies: []reply{{413, "text/html", nginx}}})
	code, body := postAs(t, New(), "", `{"model":"a/m","messages":[{"role":"user","content":"hi"}]}`)
	if code != 413 || !piOverflow.MatchString(body) || !strings.Contains(body, "byte limit") || strings.Contains(body, "context_length_exceeded") {
		t.Fatalf("byte cap on the chat route: %d %s", code, body)
	}
}
