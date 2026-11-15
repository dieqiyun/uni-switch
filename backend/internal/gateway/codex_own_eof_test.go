package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// TestCodexOwnSignInAskedAgainAfterEOF: a turn of one of Codex's own
// models, which goes to ChatGPT on Codex's own sign-in, met a connection
// closed with nothing of a reply — a fresh one, which Go's transport
// doesn't send again by itself (aetrox333 on Discord: "OpenAI can't be
// reached (Post …: EOF)" after 43ms, every time, through a fake-IP
// proxy). It is asked again on a new connection, as the accounts loop asks
// its last account again after the same 502, and Codex has the reply.
func TestCodexOwnSignInAskedAgainAfterEOF(t *testing.T) {
	codexSignedIn(t)
	pause := retryPause
	retryPause = time.Millisecond
	t.Cleanup(func() { retryPause = pause })
	var posts atomic.Int64
	h := func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		if posts.Add(1) == 1 {
			// closed before a byte of a reply, on the connection's first
			// request
			c, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				c.Close()
			}
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`data: {"type":"response.created","response":{"id":"r1","model":"gpt-6-luna"}}`,
			`data: {"type":"response.output_text.delta","delta":"pong"}`,
			`data: {"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":7,"output_tokens":1}}}`))
	}
	up := httptest.NewServer(http.HandlerFunc(h))
	t.Cleanup(up.Close)
	forgetCodexLists(t)
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })

	if err := provider.Save(provider.Provider{ID: "codex", Models: []string{"gpt-6-luna"}}); err != nil {
		t.Fatal(err)
	}

	s := New()
	// the bare name goes on Codex's own sign-in; codex/ through the
	// accounts loop, which asked again already
	for _, m := range []string{"gpt-6-luna", "codex/gpt-6-luna"} {
		posts.Store(0)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(`{"model":"`+m+`","stream":true,"input":"ping"}`))
		req.Header.Set("Authorization", "Bearer chatgpt-token")
		req.Header.Set("chatgpt-account-id", "acct-1")
		req.Header.Set("Accept", "text/event-stream")
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "pong") {
			t.Fatalf("%s: status %d after a dropped connection, want the reply asked again for: %.300s", m, rec.Code, rec.Body.String())
		}
		if n := posts.Load(); n != 2 {
			t.Fatalf("%s: ChatGPT was asked %d times, want 2 (the dropped try and one more)", m, n)
		}
	}
}

// TestCodexOwnSignInEOFTold: a connection dropped on every try is asked
// again only as many times as an overload is, and Codex is then told
// OpenAI couldn't be reached, as before.
func TestCodexOwnSignInEOFTold(t *testing.T) {
	codexSignedIn(t)
	pause := retryPause
	retryPause = time.Millisecond
	t.Cleanup(func() { retryPause = pause })
	var posts atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		posts.Add(1)
		if c, _, err := w.(http.Hijacker).Hijack(); err == nil {
			c.Close()
		}
	}))
	t.Cleanup(up.Close)
	forgetCodexLists(t)
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })

	s := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(`{"model":"gpt-6-luna","stream":false,"input":"ping"}`))
	req.Header.Set("Authorization", "Bearer chatgpt-token")
	req.Header.Set("chatgpt-account-id", "acct-1")
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 502 || !strings.Contains(rec.Body.String(), "OpenAI can't be reached") {
		t.Fatalf("status %d, want 502 saying OpenAI can't be reached: %.300s", rec.Code, rec.Body.String())
	}
	if n := posts.Load(); n != int64(lastRetries)+1 {
		t.Fatalf("ChatGPT was asked %d times, want %d", n, lastRetries+1)
	}
}
