package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Codex kept signed in to an account of the user's choosing (#524: the
// first is only the one whose allowance is spent first, not the one the
// user uses Codex as): the gateway still tries the first first, In order,
// and the one Codex is signed in to at its own place behind it.
func TestKeptLoginStandsAtItsPlace(t *testing.T) {
	codexSignedIn(t, "spare@example.com")
	// a usage read the gateway starts behind a reply reaches the same server
	// on a goroutine of its own, so only the replies are counted, under mu
	var (
		mu    sync.Mutex
		tried []string
	)
	replies := func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), tried...) }
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "/responses") {
			mu.Lock()
			tried = append(tried, r.Header.Get("chatgpt-account-id"))
			mu.Unlock()
		}
		io.WriteString(w, sse(
			`data: {"type":"response.created","response":{"id":"r1","model":"gpt-5.5"}}`,
			`data: {"type":"response.output_text.delta","delta":"pong"}`,
			`data: {"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":7,"output_tokens":1}}}`))
	}))
	t.Cleanup(up.Close)
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })
	if err := provider.SetRouting("codex", provider.Ordered); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetKeepLoginAs("codex", "spare@example.com"); err != nil {
		t.Fatal(err)
	}
	for _, l := range provider.Logins("codex") {
		if l.Active != (l.User == "spare@example.com") {
			t.Fatalf("kept on spare, signed in as %+v", l)
		}
	}
	code, body := codexPost(t, `{"model":"gpt-5.5","stream":true,"input":"ping"}`)
	if code != 200 || strings.Join(replies(), ",") != "acct-1" {
		t.Fatalf("%d %s: tried %v, not the first (acct-1)", code, body, replies())
	}
	if got := provider.InUseLogin("codex"); got != "me@example.com" {
		t.Fatalf("in use %q", got)
	}
	// the account a model's note names as first (#1957) is the one tried
	if got := provider.FirstLogin("codex"); got != "me@example.com" {
		t.Fatalf("first %q", got)
	}
	// let go, Codex is signed in to the first again, and it goes first
	if err := provider.SetKeepLogin("codex", false); err != nil {
		t.Fatal(err)
	}
	for _, l := range provider.Logins("codex") {
		if l.Active != (l.User == "me@example.com") {
			t.Fatalf("let go, signed in as %+v", l)
		}
	}
	mu.Lock()
	tried = nil
	mu.Unlock()
	codexPost(t, `{"model":"gpt-5.5","stream":true,"input":"ping"}`)
	if strings.Join(replies(), ",") != "acct-1" {
		t.Fatalf("let go, tried %v", replies())
	}
}
