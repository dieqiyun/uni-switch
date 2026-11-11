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

// A model served only on /responses is asked on Chat first and answered on
// Responses (forwardTranslated's endpoint fallback, Copilot's newest
// models): the try's protocol still reads Chat, so the summary the reply
// streamed is only known for one through the upstream that sent it
// (streamTranslated's summaryUpstream). With no reasoning count, a summary
// that went unmarked would time the hidden reasoning's tokens as a tool
// call's speed (#1463).
func TestSummaryAfterEndpointFallbackStaysHidden(t *testing.T) {
	fresh(t)
	summary := &paced{events: []string{
		`data: {"type":"response.created","response":{"id":"r1","status":"in_progress","output":[]}}`,
		`~data: {"type":"response.reasoning_summary_text.delta","delta":"first summary"}`,
		`~data: {"type":"response.reasoning_summary_text.delta","delta":"second summary"}`,
		`~data: {"type":"response.reasoning_summary_text.delta","delta":"third summary"}`,
		`data: {"type":"response.output_item.added","item":{"type":"function_call","call_id":"c1","name":"check"}}`,
		`data: {"type":"response.function_call_arguments.delta","delta":"{}"}`,
		`data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":1500}}}`,
	}, gap: 150 * time.Millisecond}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":{"message":"model \"m1\" is not accessible via the /chat/completions endpoint","code":"unsupported_api_for_model"}}`)
			return
		}
		summary.ServeHTTP(w, r)
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Chat: up.URL + "/v1", Responses: up.URL + "/v1", Key: "k", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	s := New()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"fake/m1","stream":true,"max_tokens":300,"messages":[{"role":"user","content":"hi"}]}`)))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "check") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	u := lastUsage(t)
	if u.TTFT <= 0 || u.Output != 1500 {
		t.Fatalf("the reply wasn't timed as a stream: %+v", u)
	}
	if n, w := u.Decode(); !u.ReasoningHidden || n != 0 || w != 0 {
		t.Fatalf("a Responses summary was timed as the reply's speed (%d tokens in %d ms): %+v", n, w, u)
	}
}
