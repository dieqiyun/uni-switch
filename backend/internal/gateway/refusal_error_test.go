package gateway

import (
	"encoding/json"
	"io"

	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The message is what #2400's agent showed, word for word; no vendor's
// reply was captured, so the envelopes around it below are each protocol's
// error shapes, not a vendor's bytes.
const highRiskMessage = "The request was rejected because it was considered high risk"

func TestHighRiskErrorMessage(t *testing.T) {
	for _, msg := range []string{highRiskMessage, " \n" + highRiskMessage + "\n"} {
		b, _ := json.Marshal(map[string]any{"error": map[string]any{"message": msg}})
		if note, ok := policyRefusal(b); !ok || !strings.Contains(note, highRiskMessage) {
			t.Errorf("not classified as a refusal: %s: %q", b, note)
		}
	}
	for _, msg := range []string{
		"high risk", "not " + highRiskMessage, highRiskMessage + "; please explain this message",
		`The server quoted "` + highRiskMessage + `"`, "invalid API key",
	} {
		b, _ := json.Marshal(map[string]any{"error": map[string]any{"message": msg}})
		if note, ok := policyRefusal(b); ok {
			t.Errorf("unrelated error classified as a refusal: %s: %q", b, note)
		}
	}
	for _, event := range []string{
		`data: {"error":{"message":"` + highRiskMessage + `"}}`,
		`event: error` + "\n" + `data: {"message":"` + highRiskMessage + `"}`,
		`data: {"type":"error","error":{"type":"invalid_request_error","message":"` + highRiskMessage + `"}}`,
		`data: {"type":"response.failed","response":{"error":{"message":"` + highRiskMessage + `"}}}`,
	} {
		if kind, status, msg := streamEvent([]byte(event)); kind == eventError || status != refusedStatus || !strings.Contains(msg, highRiskMessage) {
			t.Errorf("error event was not a refusal: %s: kind %d, status %d, message %q", event, kind, status, msg)
		}
	}
}

func refusalErrorAsk(from provider.Protocol) (string, string) {
	switch from {
	case provider.Responses:
		return "/v1/responses", codexAsk
	case provider.Anthropic:
		return "/v1/messages", `{"model":"group/g","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"hi"}]}`
	default:
		return "/v1/chat/completions", `{"model":"group/g","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	}
}

func sendRefusalError(t *testing.T, s *Server, path, body string) (int, string) {
	t.Helper()
	gw := httptest.NewServer(localGuard(s.Handler()))
	defer gw.Close()
	client := gw.Client()
	client.Timeout = 10 * time.Second // deadlock guard for the held-open upstream
	res, err := client.Post(gw.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request did not finish without upstream EOF: %v", err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(b)
}

func TestHighRiskErrorSplitAfterContent(t *testing.T) {
	const start = "data: {\"choices\":[{\"delta\":{\"content\":\"half\"}}]}\n\n"
	for _, x := range []struct {
		name    string
		frame   string
		refused bool
	}{
		{"named error", sse("event: error\n" + `data: {"message":"` + highRiskMessage + `"}`), true},
		{"named coded error", sse("event: error\n" + `data: {"code":"content_filter","message":"filtered"}`), true},
		{"nested error", sse(`data: {"error":{"message":"` + highRiskMessage + `"}}`), true},
		{"quoted error", sse(`data: {"choices":[{"delta":{"content":"The error says: ` + highRiskMessage + `"}}]}`), false},
	} {
		t.Run(x.name, func(t *testing.T) {
			for split := 0; split <= len(x.frame); split++ {
				rec := httptest.NewRecorder()
				h := newHoldWriter(rec, true)
				h.Header().Set("Content-Type", "text/event-stream")
				stopped := false
				h.stop = func() { stopped = true }
				io.WriteString(h, start)
				io.WriteString(h, x.frame[:split])
				io.WriteString(h, x.frame[split:])
				if h.refusedAfter != x.refused || h.refused || stopped || !h.passing || rec.Body.String() != start+x.frame {
					t.Fatalf("split %d of %q: late refusal %v, held refusal %v, stopped %v, passing %v, body %q", split, x.frame, h.refusedAfter, h.refused, stopped, h.passing, rec.Body.String())
				}
			}
		})
	}
}
