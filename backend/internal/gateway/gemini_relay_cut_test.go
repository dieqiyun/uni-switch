package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A Gemini client's stream relayed as it comes (a custom provider's Gemini
// URL, #1346, or Vertex AI) that the upstream stopped before a candidate's
// finishReason ends in Gemini's own error, as an Anthropic or Responses
// one cut short does in theirs (#370), not as a reply that finished
// (#1644, ggbdpq). Vertex AI's own chunks, usageMetadata on each.
func TestGeminiRelayEndedShort(t *testing.T) {
	relay := func(reply string) (int, string) {
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
		return post(t, "/v1beta/models/up/gemini-3.8-flash:streamGenerateContent?alt=sse",
			`{"contents":[{"role":"user","parts":[{"text":"Is 391 prime?"}]}]}`)
	}

	code, body := relay(vertexCut)
	if code != 200 || !strings.Contains(body, "because it is") {
		t.Fatalf("status %d: %s", code, body)
	}
	const cut = `data: {"error":{"code":502,"message":"UP: the reply ended before it was complete","status":"UNAVAILABLE"}}`
	if !strings.HasSuffix(strings.TrimSpace(body), cut) {
		t.Errorf("a cut relay ended as if whole: %s", body)
	}
	if strings.Contains(body, "event:") {
		t.Errorf("an event name Google's SDK doesn't read: %s", body)
	}

	// the whole reply, and one that said nothing but its usage (#667's to
	// answer), go as they came
	for what, reply := range map[string]string{
		"whole":      vertexThought,
		"usage only": "data: {\"usageMetadata\":{\"promptTokenCount\":5}}\r\n\r\n",
	} {
		code, body := relay(reply)
		if code != 200 || strings.Contains(body, "ended before") {
			t.Errorf("%s: %d %s", what, code, body)
		}
	}
}
