package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// One of Codex's own models, when OpenAI can't be reached, fails saying so
// and what to do, not with the dial error alone (#322).
func TestCodexOwnModelUnreached(t *testing.T) {
	setup(t, provider.Chat, &fake{t: t})
	// port 1: refused at once, and no test can listen on it. A closed
	// server's port was handed to another test's server on Linux CI, whose
	// fake answered "pong" with 200.
	was := codexAPIBase
	codexAPIBase = "http://127.0.0.1:1"
	t.Cleanup(func() { codexAPIBase = was })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(`{"model":"gpt-5.5","input":"hi","stream":true}`))
	req.Header.Set("Authorization", "Bearer sk-relay-key")
	New().Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != 502 || !strings.Contains(body, "OpenAI can't be reached") || !strings.Contains(body, "gpt-5.5 is one of Codex's own models") ||
		!strings.Contains(body, "pick one of uni-switch's models") {
		t.Fatalf("%d %s", rec.Code, body)
	}
}
