package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// twoAccounts stands in for the ChatGPT backend with Codex signed in to
// me@example.com (acct-1) and spare@example.com (acct-2) on beside it;
// each answers with its own account id, and the requests are noted.
func twoAccounts(t *testing.T) *[]http.Header {
	t.Helper()
	codexSignedIn(t, "spare@example.com")
	var heads []http.Header
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		heads = append(heads, r.Header.Clone())
		io.WriteString(w, sse(
			`data: {"type":"response.created","response":{"id":"r1","model":"gpt-5.5"}}`,
			`data: {"type":"response.output_text.delta","delta":"from `+r.Header.Get("chatgpt-account-id")+`"}`,
			`data: {"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":7,"output_tokens":1}}}`))
	}))
	t.Cleanup(up.Close)
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })
	return &heads
}

// pinnedPost asks for model at path, uni-switch's /v1/responses or Codex's
// own endpoint, pinned to account.
func pinnedPost(t *testing.T, s *Server, path, model, account string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", path, strings.NewReader(`{"model":"`+model+`","stream":true,"input":"ping"}`))
	req.Header.Set("Authorization", "Bearer uni-switch")
	if path != "/v1/responses" {
		req.Header.Set("Authorization", "Bearer chatgpt-token")
		req.Header.Set("chatgpt-account-id", "acct-1")
	}
	req.Header.Set(AccountHeader, account)
	s.Handler().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

var pinPaths = map[string]string{"/v1/responses": "codex/gpt-5.5", CodexPath + "/responses": "gpt-5.5"}

// X-Uni-Switch-Account names the account that answers, over the one routing
// would pick first (acct-1, Codex's own), on uni-switch's endpoint and on
// Codex's; the header isn't sent on, and the trace says it was pinned.
func TestAccountPinned(t *testing.T) {
	heads := twoAccounts(t)
	s := New()
	for path, model := range pinPaths {
		*heads = nil
		code, body := pinnedPost(t, s, path, model, "Spare@example.com")
		if code != 200 || !strings.Contains(body, "from acct-2") {
			t.Fatalf("%s: %d %s", path, code, body)
		}
		if len(*heads) != 1 || (*heads)[0].Get("chatgpt-account-id") != "acct-2" {
			t.Fatalf("%s: upstream %v", path, *heads)
		}
		if (*heads)[0].Get(AccountHeader) != "" {
			t.Errorf("%s: %s went upstream", path, AccountHeader)
		}
		r := s.trace.routes[len(s.trace.routes)-1]
		if r.Pinned != "Spare@example.com" || len(r.Order) != 1 || r.Order[0].Who != "spare@example.com" {
			t.Errorf("%s: route pinned %q order %+v", path, r.Pinned, r.Order)
		}
	}
	// the first account, by its user, is pinned as well
	*heads = nil
	if code, body := pinnedPost(t, s, "/v1/responses", "codex/gpt-5.5", "me@example.com"); code != 200 || !strings.Contains(body, "from acct-1") {
		t.Fatalf("own account: %d %s", code, body)
	}
}

// An account uni-switch doesn't have is an error, not another account's reply.
func TestAccountPinnedUnknown(t *testing.T) {
	heads := twoAccounts(t)
	s := New()
	for path, model := range pinPaths {
		code, body := pinnedPost(t, s, path, model, "nobody@example.com")
		if code != 404 || !strings.Contains(body, "nobody@example.com") || !strings.Contains(body, "spare@example.com") {
			t.Fatalf("%s: %d %s", path, code, body)
		}
	}
	if len(*heads) != 0 {
		t.Fatalf("asked upstream: %v", *heads)
	}
}

// A resting account pinned is an error saying so, with the other account,
// ready, not asked in its place.
func TestAccountPinnedResting(t *testing.T) {
	heads := twoAccounts(t)
	restingUntil.Lock()
	restingUntil.m["codex@me@example.com"] = time.Now().Add(time.Hour)
	restingUntil.note["codex@me@example.com"] = Rest{Why: failQuota}
	restingUntil.Unlock()
	t.Cleanup(func() {
		restingUntil.Lock()
		delete(restingUntil.m, "codex@me@example.com")
		delete(restingUntil.note, "codex@me@example.com")
		restingUntil.Unlock()
	})
	s := New()
	for path, model := range pinPaths {
		code, body := pinnedPost(t, s, path, model, "me@example.com")
		if code != 429 || !strings.Contains(body, "rests until") {
			t.Fatalf("%s: %d %s", path, code, body)
		}
	}
	if len(*heads) != 0 {
		t.Fatalf("asked upstream: %v", *heads)
	}
	// unpinned, the other one answers
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"codex/gpt-5.5","stream":true,"input":"ping"}`))
	req.Header.Set("Authorization", "Bearer uni-switch")
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "from acct-2") {
		t.Fatalf("unpinned: %d %s", rec.Code, rec.Body.String())
	}
}

// An account whose user isn't ASCII (StockRabbit's 北冥, plugins#615) is
// named percent-encoded, as fetch can send no other header value; it is
// pinned as its user, on uni-switch's endpoint and on Codex's, and the trace
// names it decoded.
func TestAccountPinnedPercentEncoded(t *testing.T) {
	heads := twoAccounts(t)
	codexSignedIn(t, "北冥")
	s := New()
	for path, model := range pinPaths {
		*heads = nil
		code, body := pinnedPost(t, s, path, model, url.PathEscape("北冥"))
		if code != 200 || !strings.Contains(body, "from acct-2") {
			t.Fatalf("%s: %d %s", path, code, body)
		}
		if len(*heads) != 1 || (*heads)[0].Get("chatgpt-account-id") != "acct-2" {
			t.Fatalf("%s: upstream %v", path, *heads)
		}
		if r := s.trace.routes[len(s.trace.routes)-1]; r.Pinned != "北冥" || len(r.Order) != 1 || r.Order[0].Who != "北冥" {
			t.Errorf("%s: route pinned %q order %+v", path, r.Pinned, r.Order)
		}
	}
	// one it doesn't have is named decoded in the error
	code, body := pinnedPost(t, s, "/v1/responses", "codex/gpt-5.5", url.PathEscape("南海"))
	if code != 404 || !strings.Contains(body, "南海") || !strings.Contains(body, "北冥") {
		t.Fatalf("unknown: %d %s", code, body)
	}
	// a value that isn't an encoding is the name as written
	if got := pinnedName("50%off"); got != "50%off" {
		t.Errorf("pinnedName(50%%off) = %q", got)
	}
	if got := pinnedName("%E5%8C"); got != "%E5%8C" {
		t.Errorf("pinnedName(half a rune) = %q", got)
	}
}
