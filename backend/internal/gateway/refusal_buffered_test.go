package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The ChatGPT backend holding a gpt-6.x reply back for extra safety checks,
// as it did for Koohoko (#248): it says so in a response.metadata event,
// reasons a while, and then fails the response with bio_policy.
var bufferedStart = []string{
	`event: response.created` + "\n" + `data: {"type":"response.created","response":{"id":"r0","status":"in_progress"}}`,
	`event: response.metadata` + "\n" + `data: {"type":"response.metadata","response_id":"r0","metadata":{"type":"safety_buffering","use_cases":["bio"],"reasons":["user_risk"]}}`,
	`event: response.in_progress` + "\n" + `data: {"type":"response.in_progress","response":{"id":"r0","status":"in_progress"}}`,
}

const bioFailed = `event: response.failed` + "\n" + `data: {"type":"response.failed","response":{"id":"r0","status":"failed","error":{"code":"bio_policy","message":"This content was flagged for possible biological risk."}}}`

// A refusal after the backend's response.metadata — safety buffering, or
// its moderation metadata — goes on to the next account: before, the
// metadata was taken for the reply's content, the stream let through, and
// Codex was handed the bio_policy failure ("This content can't be shown")
// with a 200 and the next account never asked.
func TestRefusalAfterResponseMetadataMovesToNextAccount(t *testing.T) {
	for _, x := range []struct {
		name string
		lead []string
	}{
		{"safety buffering", bufferedStart},
		{"moderation metadata", []string{bufferedStart[0],
			`data: {"type":"response.metadata","metadata":{"openai_chatgpt_moderation_metadata":{"presentation":"inline"}}}`}},
	} {
		t.Run(x.name, func(t *testing.T) {
			codexSignedIn(t, "spare@example.com")
			if err := provider.SetRouting("codex", provider.Ordered); err != nil {
				t.Fatal(err)
			}
			var tried []string
			chatgptRefusing(t, &tried, func(w http.ResponseWriter) {
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, sse(append(append([]string{}, x.lead...), bioFailed)...))
			})
			s := New()
			code, body := codexAskOn(s, `{"model":"gpt-5.5","stream":true,"input":"ping"}`)
			if code != 200 || !strings.Contains(body, "pong") || strings.Contains(body, "bio_policy") || strings.Join(tried, ",") != "acct-1,acct-2" {
				t.Fatalf("status %d: %s (tried %v)", code, body, tried)
			}
			if r := lastRoute(s); len(r.Tries) != 2 || r.Tries[0].Fail != failRefused || r.Tries[1].Status != 200 {
				t.Fatalf("tries: %+v", r.Tries)
			}
		})
	}
}

// A stream the backend said it holds back for safety checks is held past
// the 15s a stream of empty frames is otherwise held for: gpt-6.1-sol at
// xhigh said nothing for 35s before its bio_policy (#248). One that said
// nothing of it is let through as before.
func TestSafetyBufferedStreamHeldLonger(t *testing.T) {
	held := func(lead []string) (*holdWriter, *httptest.ResponseRecorder) {
		rec := httptest.NewRecorder()
		h := newHoldWriter(rec, true)
		h.Header().Set("Content-Type", "text/event-stream")
		h.WriteHeader(200)
		io.WriteString(h, sse(lead[:len(lead)-1]...))
		h.since = time.Now().Add(-(holdLongest + 5*time.Second))
		io.WriteString(h, sse(lead[len(lead)-1]))
		return h, rec
	}
	h, rec := held(bufferedStart)
	if h.passing || rec.Body.Len() != 0 {
		t.Fatalf("let through after %s: %q", holdLongest, rec.Body.String())
	}
	io.WriteString(h, sse(bioFailed))
	if !h.failed() || !h.refused || !strings.Contains(h.failMsg, "bio_policy") || rec.Body.Len() != 0 {
		t.Fatalf("failed %v refused %v %q; sent %q", h.failed(), h.refused, h.failMsg, rec.Body.String())
	}

	h, rec = held([]string{bufferedStart[0], bufferedStart[2]})
	if !h.passing || !strings.Contains(rec.Body.String(), "response.in_progress") {
		t.Fatalf("a stream without safety buffering held past %s", holdLongest)
	}
}
