package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// codexErrorMessage is error.message of a reply, as Codex reads it
// (codex-rs UnexpectedResponseError::extract_error_message): "" is what
// Codex shows as "Unknown error" when the body is empty too.
func codexErrorMessage(body string) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	json.Unmarshal([]byte(body), &e)
	return strings.TrimSpace(e.Error.Message)
}

// outrageous_piglet on Discord: Codex's compaction ended "unexpected
// status 502 Bad Gateway: Unknown error", which Codex says of an error
// status whose body is empty. A compaction of Codex's own model relayed
// to OpenAI (/responses/compact) went back with OpenAI's body as it came,
// nothing in it, or a web page; it is said now, as a turn's is (#409).
func TestCodexOwnCompactFailureIsSaid(t *testing.T) {
	for _, path := range []string{"/responses/compact", "/responses"} {
		for _, b := range []string{"", "\n", "<html><head><title>502 Bad Gateway</title></head><body>cloudflare</body></html>"} {
			setup(t, provider.Chat, &fake{t: t})
			chatgpt(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
				io.WriteString(w, b)
			})
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("POST", CodexPath+path, strings.NewReader(`{"model":"gpt-6-sol","stream":true,"input":[{"type":"message","role":"user","content":"remember this"},{"type":"compaction_trigger"}]}`))
			New().Handler().ServeHTTP(rec, req)
			msg := codexErrorMessage(rec.Body.String())
			if rec.Code != http.StatusBadGateway || !strings.Contains(msg, "502") || !strings.Contains(msg, "gpt-6-sol") {
				t.Errorf("%s %q: %d %q", path, b, rec.Code, rec.Body)
			}
			if path == "/responses/compact" && !strings.Contains(msg, "compaction") {
				t.Errorf("%s %q doesn't say it was the compaction: %q", path, b, msg)
			}
			if strings.HasPrefix(b, "<html>") && (!strings.Contains(msg, "web page") || !strings.Contains(msg, "“502 Bad Gateway”")) {
				t.Errorf("%s: the page isn't named: %q", path, msg)
			}
		}
	}
}

// An error OpenAI says in words goes to Codex as it came.
func TestCodexOwnCompactErrorAsSaid(t *testing.T) {
	setup(t, provider.Chat, &fake{t: t})
	const said = `{"error":{"message":"busy now","type":"server_error"}}`
	chatgpt(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, said)
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", CodexPath+"/responses/compact", strings.NewReader(`{"model":"gpt-6-sol","input":[{"type":"message","role":"user","content":"hi"}]}`))
	New().Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway || rec.Body.String() != said {
		t.Errorf("%d %q", rec.Code, rec.Body)
	}
}

// A uni-switch model's summary request that fails goes back said as the
// compaction's, with who answered it: "Fake: 502 Bad Gateway" alone read
// as Codex's own failure, whatever the provider said or didn't.
func TestCodexCompactFailureSaysCompaction(t *testing.T) {
	for _, b := range []string{"", "<html>bad gateway</html>", `{"error":{"message":"upstream overloaded","code":"overloaded"}}`} {
		f := &fake{t: t, refuse: func([]byte) (int, string) { return http.StatusBadGateway, b }}
		setup(t, provider.Responses, f)
		code, body := codexPost(t, compact404Request)
		msg := codexErrorMessage(body)
		if code != http.StatusBadGateway || !strings.HasPrefix(msg, "compaction: ") || !strings.Contains(msg, "fake (fake/m1") || !strings.Contains(msg, "502") {
			t.Errorf("%q: %d %s", b, code, body)
		}
		if strings.Contains(b, "overloaded") && (!strings.Contains(msg, "upstream overloaded") || !strings.Contains(body, `"code":"overloaded"`)) {
			t.Errorf("the vendor's words or code are lost: %s", body)
		}
	}
}

// A provider with no summary request to answer there — 405 or 501, as a
// relay's 404 (#866) — is asked again as text, then compacted without a
// model, so Codex's compaction completes.
func TestCodexCompactNoEndpointCompactsLocally(t *testing.T) {
	for _, status := range []int{http.StatusMethodNotAllowed, http.StatusNotImplemented} {
		f := &fake{t: t, refuse: func([]byte) (int, string) { return status, "" }}
		setup(t, provider.Responses, f)
		code, body := codexPost(t, compact404Request)
		if code != 200 {
			t.Fatalf("%d: %d %s", status, code, body)
		}
		if got := compactedText(t, body); !strings.Contains(got, "fix the login bug") || !strings.Contains(got, "now add a test") {
			t.Errorf("%d: local summary %q", status, got)
		}
		if f.calls != 2 {
			t.Errorf("%d: asked %d times, want the summary and its plain retry", status, f.calls)
		}
	}
}
