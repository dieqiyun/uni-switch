package gateway

import (
	"net/http"
	"net/http/httptest"

	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// agyTurn sends one chat turn for an Antigravity model to s.
func agyTurn(s *Server) (int, string) {
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"antigravity/gemini-3.8-flash","stream":false,"messages":[{"role":"user","content":"hi"}]}`)))
	return rec.Code, rec.Body.String()
}

// The same for any vendor's account restriction (here uni-switch's words for
// ZCode's risk control, on a ChatGPT account): the lone account resting
// as restricted is not asked by the next requests.
func TestRestrictedLoneAccountNotAskedWhileItRests(t *testing.T) {
	codexSignedIn(t)
	var tried []string
	chatgptRefusing(t, &tried, riskBlocked)
	s := New()
	for n := 1; n <= 3; n++ {
		code, body := postAs(t, s, "", `{"model":"codex/gpt-5.5","messages":[{"role":"user","content":"hi"}]}`)
		if code != http.StatusForbidden || !strings.Contains(body, provider.ZCodeRestrictedHint) || strings.Join(tried, ",") != "acct-1" {
			t.Fatalf("request %d: %d %s; asked %v", n, code, body, tried)
		}
	}
}
