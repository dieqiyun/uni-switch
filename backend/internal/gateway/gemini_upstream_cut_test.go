package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A translated reply from a Gemini upstream (a custom provider's Gemini
// URL, Vertex AI) whose stream stops before a candidate's finishReason is
// a reply cut short, as an Anthropic one with no message_stop or a
// Responses one with no response.completed is (#1562, #1563): the client
// is told so, not handed the part it got as finished (#1643, ggbdpq).
// Vertex AI sends usageMetadata on every chunk, so the cut here is of its
// own reply (vertexThought), not a sample without it.

// vertexCut is vertexThought stopped a few words into its answer: its
// thought and "No, 391 is not prime because it is", without the rest and
// the last chunk, the one carrying the finishReason.
var vertexCut = vertexThought[:strings.Index(vertexThought, `data: {"candidates": [{"content": {"role": "model","parts": [{"text": " divisible`)]

// askCutGemini serves path's request body from a custom provider's Gemini URL
// answering with reply as it is.
func askCutGemini(t *testing.T, reply, path, body string) (int, string) {
	t.Helper()
	fresh(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, reply)
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "up", Name: "UP", Key: "k", Models: []string{"gemini-3.8-flash"}, Gemini: up.URL + "/v1beta"}); err != nil {
		t.Fatal(err)
	}
	return post(t, path, body)
}

const (
	geminiChatStream = `{"model":"up/gemini-3.8-flash","stream":true,"messages":[{"role":"user","content":"Is 391 prime?"}]}`
	geminiChatWhole  = `{"model":"up/gemini-3.8-flash","messages":[{"role":"user","content":"Is 391 prime?"}]}`
	geminiMessages   = `{"model":"up/gemini-3.8-flash","stream":true,"max_tokens":256,"messages":[{"role":"user","content":"Is 391 prime?"}]}`
)

func TestGeminiUpstreamEndedShort(t *testing.T) {
	if !strings.Contains(vertexCut, "not prime because it is") || strings.Contains(vertexCut, "divisible") || strings.Contains(vertexCut, "finishReason") {
		t.Fatalf("vertexCut is not the reply cut: %s", vertexCut)
	}
	code, body := askCutGemini(t, vertexCut, "/v1/chat/completions", geminiChatStream)
	if code != 200 || !strings.Contains(body, "ended before it was complete") || chatFinish(body) == "stop" {
		t.Errorf("Chat: a cut reply finished as whole (%d): %s", code, body)
	}
	code, body = askCutGemini(t, vertexCut, "/v1/messages", geminiMessages)
	if code != 200 || !strings.Contains(body, "ended before it was complete") || strings.Contains(body, "end_turn") {
		t.Errorf("Anthropic: a cut reply finished as whole (%d): %s", code, body)
	}

	// the whole reply, its finishReason on the last chunk, goes as it came
	code, body = askCutGemini(t, vertexThought, "/v1/chat/completions", geminiChatStream)
	if code != 200 || chatFinish(body) != "stop" || strings.Contains(body, "ended before") {
		t.Errorf("whole: %d %s", code, body)
	}
}

// A client that asked for no stream is told the same 502, rather than
// handed the cut as its whole reply.
func TestGeminiNonStreamEndedShort(t *testing.T) {
	code, body := askCutGemini(t, vertexCut, "/v1/chat/completions", geminiChatWhole)
	if code != 502 || !strings.Contains(body, "ended before it was complete") {
		t.Errorf("a cut reply served whole (%d): %s", code, body)
	}
	code, body = askCutGemini(t, vertexThought, "/v1/chat/completions", geminiChatWhole)
	if code != 200 || !strings.Contains(body, "divisible by 17 and 23") || strings.Contains(body, "ended before") {
		t.Errorf("whole: %d %s", code, body)
	}
}

func TestGeminiEnds(t *testing.T) {
	for chunk, want := range map[string]bool{
		// Vertex AI's usage on every chunk is no end
		`{"candidates": [{"content": {"role": "model","parts": [{"text": "No, 391"}]}}],"usageMetadata": {"trafficType": "ON_DEMAND"}}`: false,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]}}]}`:                                                         false,
		`{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP"}]}`:                                                true,
		`{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"MAX_TOKENS"}],"usageMetadata":{"totalTokenCount":5}}`:    true,
		// Code Assist wraps its chunk in a response
		`{"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"Reading."}]}}],"usageMetadata":{"promptTokenCount":100}}}`: false,
		`{"response":{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP"}]},"traceId":"t"}`:                         true,
		// no candidate at all: the reply said nothing, the empty reply's (#667)
		`{"usageMetadata":{"promptTokenCount":0}}`:                true,
		`{"promptFeedback":{"blockReason":"PROHIBITED_CONTENT"}}`: true,
		`{"response":{"usageMetadata":{"promptTokenCount":0}}}`:   true,
		`not json`: false,
	} {
		if got := geminiEnds([]byte(chunk)); got != want {
			t.Errorf("geminiEnds(%s) = %v, want %v", chunk, got, want)
		}
	}
}
