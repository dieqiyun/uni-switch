package gateway

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// edgeDropped is what ylorn's ChatGPT backend answered to a request whose
// screenshot took it past about 1 MiB (Discord, v0.1.1179), and
// ScarfQian's to a 1.1 MiB computer-use turn (#1628): Envoy's own 503
// behind Cloudflare, as text, before any reply.
const edgeDropped = "upstream connect error or disconnect/reset before headers. reset reason: remote connection failure"

// dropsBig stands in for that ChatGPT backend: a body past 1 MiB is
// answered with edgeDropped the first *drops times (every time while it
// is below zero) and served after; a smaller one is served. It notes the
// account asked each time.
func dropsBig(t *testing.T, tried *[]string, drops *int) {
	t.Helper()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*tried = append(*tried, r.Header.Get("chatgpt-account-id"))
		if len(b) > 1<<20 && *drops != 0 {
			if *drops > 0 {
				*drops--
			}
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Server", "cloudflare")
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, edgeDropped)
			return
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
}

// screenshotTurn is a Responses turn with a PNG screenshot of about n
// bytes inline, base64 as an agent sends it. Its bytes aren't an image
// any decoder reads, so nothing on the way makes it smaller.
func screenshotTurn(t *testing.T, n int) string {
	t.Helper()
	raw := make([]byte, n*3/4)
	rand.Read(raw)
	b, _ := json.Marshal(map[string]any{"model": "gpt-5.5", "stream": true, "input": []any{
		map[string]any{"type": "message", "role": "user", "content": []any{
			map[string]any{"type": "input_text", "text": "what does this screenshot show?"},
			map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)},
		}}}})
	return string(b)
}

// ScarfQian (#1628): with two ChatGPT accounts on, a 1.2 MiB turn the
// backend closes on twice, then answers, is answered: asked of acct-1,
// then acct-2, then acct-2 again, and nobody rests, since no account is at
// fault for it. On a2b96139c's code the agent got a 413 after one try,
// which Codex never asks again.
func TestCodexResetBigBodyAskedAgain(t *testing.T) {
	codexSignedIn(t, "spare@example.com")
	var tried []string
	drops := 2
	dropsBig(t, &tried, &drops)

	code, body := codexPost(t, screenshotTurn(t, 1200<<10))
	if code != 200 || !strings.Contains(body, "pong") {
		t.Fatalf("status %d, want 200 pong: %s", code, body)
	}
	if strings.Join(tried, ",") != "acct-1,acct-2,acct-2" {
		t.Errorf("asked %v, want acct-1,acct-2,acct-2", tried)
	}
	if r := restingNow(t); len(r) != 0 {
		t.Errorf("rest over a reset of a large request: %v", r)
	}
}

// Closed on every time, the turn is asked of each account and the last
// again, as for a 503, and the agent is told the 503 — which Codex asks
// again for — with what may help should it keep happening. Nobody rests,
// and nothing is remembered of it: the next turn, as large, goes to acct-1
// and is answered once the backend takes it.
func TestCodexResetBigBodyStays503(t *testing.T) {
	codexSignedIn(t, "spare@example.com")
	var tried []string
	drops := -1
	dropsBig(t, &tried, &drops)

	code, body := codexPost(t, screenshotTurn(t, 1200<<10))
	if code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503: %s", code, body)
	}
	for _, want := range []string{"MiB", "compact", "image", "reset reason"} {
		if !strings.Contains(body, want) {
			t.Errorf("the agent isn't told %q: %s", want, body)
		}
	}
	if strings.Join(tried, ",") != "acct-1,acct-2,acct-2,acct-2" {
		t.Errorf("asked %v, want acct-1 then acct-2 three times", tried)
	}
	if r := restingNow(t); len(r) != 0 {
		t.Errorf("rest over a reset of a large request: %v", r)
	}

	tried, drops = nil, 0
	code, body = codexPost(t, screenshotTurn(t, 1200<<10))
	if code != 200 || !strings.Contains(body, "pong") || strings.Join(tried, ",") != "acct-1" {
		t.Fatalf("large turn after: %d %s tried %v", code, body, tried)
	}
}

// With Codex's own sign-in alone, relayed as it came: a large request
// closed on once is asked again and answered; closed on every time, Codex
// is answered the 503 after three tries, with what may help.
func TestCodexResetBigBodyRelayed(t *testing.T) {
	codexSignedIn(t)
	var tried []string
	drops := 1
	dropsBig(t, &tried, &drops)

	code, body := codexPost(t, screenshotTurn(t, 1200<<10))
	if code != 200 || !strings.Contains(body, "pong") || len(tried) != 2 {
		t.Fatalf("closed on once: %d %s tried %v", code, body, tried)
	}

	tried, drops = nil, -1
	code, body = codexPost(t, screenshotTurn(t, 1200<<10))
	if code != http.StatusServiceUnavailable || !strings.Contains(body, "compact") || !strings.Contains(body, "reset reason") {
		t.Fatalf("status %d: %s", code, body)
	}
	if len(tried) != 3 {
		t.Errorf("asked %d times, want 3", len(tried))
	}
}

// The same 503 to a small request is the backend's trouble, as it is in
// an outage: it is relayed as the 503 it is, for the client to try again.
func TestCodexEdgeResetSmallBodyStays503(t *testing.T) {
	codexSignedIn(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, edgeDropped)
	}))
	t.Cleanup(up.Close)
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })

	code, body := codexPost(t, `{"model":"gpt-5.5","stream":true,"input":"ping"}`)
	if code != http.StatusServiceUnavailable || !strings.Contains(body, "reset reason") {
		t.Fatalf("status %d: %s", code, body)
	}
}

// edgeUnbufferedSaid is the body of the 507 sparrowu93's ChatGPT backend
// answered through uni-switch in pi (#2034), which pi was told as "Codex: 507
// Insufficient Storage: exceeded request buffer limit while retrying
// upstream": Envoy's own, when its first try at the backend failed and
// the request was more than it buffers to try again.
const edgeUnbufferedSaid = "exceeded request buffer limit while retrying upstream"

// unbuffers stands in for that backend: every request is answered the 507
// the first *drops times (every time while it is below zero) and served
// after. It notes the account asked each time.
func unbuffers(t *testing.T, tried *[]string, drops *int, said string) {
	t.Helper()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		*tried = append(*tried, r.Header.Get("chatgpt-account-id"))
		if *drops != 0 {
			if *drops > 0 {
				*drops--
			}
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusInsufficientStorage)
			io.WriteString(w, said)
			return
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
}

// sparrowu93 (#2034): the 507 is the backend's failure Envoy couldn't
// retry for the body's size, as the 503 above is. With two accounts, a
// turn answered it twice, then served, is answered, and nobody rests. The
// 507 itself says the body was large, so no size is asked of it: this
// one is under codexDropBody. On the old code the agent was told the 507
// after one try.
func TestCodexUnbufferedAskedAgain(t *testing.T) {
	codexSignedIn(t, "spare@example.com")
	var tried []string
	drops := 2
	unbuffers(t, &tried, &drops, edgeUnbufferedSaid)

	code, body := codexPost(t, screenshotTurn(t, 600<<10))
	if code != 200 || !strings.Contains(body, "pong") {
		t.Fatalf("status %d, want 200 pong: %s", code, body)
	}
	if strings.Join(tried, ",") != "acct-1,acct-2,acct-2" {
		t.Errorf("asked %v, want acct-1,acct-2,acct-2", tried)
	}
	if r := restingNow(t); len(r) != 0 {
		t.Errorf("rest over Envoy's 507: %v", r)
	}
}

// Answered the 507 every time, the agent is told a 503, which pi, Codex
// and uni-switch ask again for (pi's _isRetryableError matches 503, not 507),
// with Envoy's words and what may help. Nobody rests.
func TestCodexUnbufferedTold503(t *testing.T) {
	codexSignedIn(t, "spare@example.com")
	var tried []string
	drops := -1
	unbuffers(t, &tried, &drops, edgeUnbufferedSaid)

	code, body := codexPost(t, screenshotTurn(t, 600<<10))
	if code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503: %s", code, body)
	}
	for _, want := range []string{"507", edgeUnbufferedSaid, "MiB", "compact", "image"} {
		if !strings.Contains(body, want) {
			t.Errorf("the agent isn't told %q: %s", want, body)
		}
	}
	if strings.Join(tried, ",") != "acct-1,acct-2,acct-2,acct-2" {
		t.Errorf("asked %v, want acct-1 then acct-2 three times", tried)
	}
	if r := restingNow(t); len(r) != 0 {
		t.Errorf("rest over Envoy's 507: %v", r)
	}
}

// With Codex's own sign-in alone, relayed as it came (codexUpstreamOn):
// the 507 once is asked again and answered; every time, Codex is told the
// 503 after three tries.
func TestCodexUnbufferedRelayed(t *testing.T) {
	codexSignedIn(t)
	var tried []string
	drops := 1
	unbuffers(t, &tried, &drops, edgeUnbufferedSaid)

	code, body := codexPost(t, screenshotTurn(t, 600<<10))
	if code != 200 || !strings.Contains(body, "pong") || len(tried) != 2 {
		t.Fatalf("507 once: %d %s tried %v", code, body, tried)
	}

	tried, drops = nil, -1
	code, body = codexPost(t, screenshotTurn(t, 600<<10))
	if code != http.StatusServiceUnavailable || !strings.Contains(body, edgeUnbufferedSaid) || !strings.Contains(body, "compact") {
		t.Fatalf("status %d: %s", code, body)
	}
	if len(tried) != 3 {
		t.Errorf("asked %d times, want 3", len(tried))
	}
}

// A 507 that doesn't say Envoy's words is the vendor's own, and is
// relayed as it came.
func TestCodexOther507Stays(t *testing.T) {
	codexSignedIn(t)
	var tried []string
	drops := -1
	unbuffers(t, &tried, &drops, "disk full")

	code, body := codexPost(t, `{"model":"gpt-5.5","stream":true,"input":"ping"}`)
	if code != http.StatusInsufficientStorage || !strings.Contains(body, "disk full") || strings.Contains(body, "compact") {
		t.Fatalf("status %d: %s", code, body)
	}
}
