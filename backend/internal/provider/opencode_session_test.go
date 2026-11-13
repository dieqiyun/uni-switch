package provider

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// openCodeAsked stands in for OpenCode's gateway on http.DefaultClient,
// where model lists and the Test button's probe go: it answers 400
// "Request is missing x-opencode-session" to a request naming no session,
// as the gateway does since September 2026 (luci on Discord), and keeps
// the session each request named, with its path.
func openCodeAsked(t *testing.T) func() [][2]string {
	t.Helper()
	var mu sync.Mutex
	var asked [][2]string
	rt := openCodeTransport(func(r *http.Request) (*http.Response, error) {
		if r.Body != nil {
			io.Copy(io.Discard, r.Body)
			r.Body.Close()
		}
		s := r.Header.Get("x-opencode-session")
		mu.Lock()
		asked = append(asked, [2]string{r.URL.Path, s})
		mu.Unlock()
		h := http.Header{"Content-Type": {"application/json"}}
		reply, status := `{"type":"error","error":{"type":"invalid_request_error","message":"Request is missing x-opencode-session and cannot be routed efficiently"}}`, 400
		switch {
		case s == "":
		case strings.HasSuffix(r.URL.Path, "/models"):
			reply, status = `{"object":"list","data":[{"id":"glm-5.3","object":"model"}]}`, 200
		default:
			reply, status = `{"id":"c","object":"chat.completion","model":"glm-5.3","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`, 200
		}
		return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(reply)), Request: r}, nil
	})
	oldT, oldC := http.DefaultTransport, http.DefaultClient.Transport
	http.DefaultTransport, http.DefaultClient.Transport = rt, rt
	t.Cleanup(func() { http.DefaultTransport, http.DefaultClient.Transport = oldT, oldC })
	return func() [][2]string {
		mu.Lock()
		defer mu.Unlock()
		return append([][2]string(nil), asked...)
	}
}

type openCodeTransport func(*http.Request) (*http.Response, error)

func (f openCodeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// What isn't a conversation's turn names a session too, each its own, as
// OpenCode's own stateless calls do: the model list and the Test button's
// probe of a custom provider at OpenCode Go (its URL naming a port, which
// IsOpenCode didn't see past), and whatever else Sign signs for OpenCode's
// gateway (a System One decision). A session the user set on the
// provider stays theirs; no other vendor is sent one.
func TestOpenCodeSessionOnEveryCall(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	asked := openCodeAsked(t)
	p := Provider{ID: "ocgo", Name: "OpenCode Go", Key: "k", Chat: "https://opencode.ai:443/zen/go/v1", Models: []string{"glm-5.3"}}
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	if !p.IsOpenCode() {
		t.Error("a URL naming OpenCode's port isn't OpenCode's")
	}
	rs := p.Test(context.Background())
	if len(rs) != 1 || !rs[0].OK {
		t.Fatalf("test: %+v", rs)
	}
	got := asked()
	if len(got) != 2 || got[0][0] != "/zen/go/v1/models" || got[1][0] != "/zen/go/v1/chat/completions" {
		t.Fatalf("asked: %q", got)
	}
	if !strings.HasPrefix(got[0][1], "ses_") || !strings.HasPrefix(got[1][1], "ses_") || got[0][1] == got[1][1] {
		t.Errorf("list's session %q, probe's %q", got[0][1], got[1][1])
	}

	sign := func(p Provider, u string) http.Header {
		t.Helper()
		req, _ := http.NewRequest("POST", u, nil)
		if err := p.Sign(context.Background(), req, Chat, nil); err != nil {
			t.Fatal(err)
		}
		return req.Header
	}
	zen := Provider{ID: "zen", Key: "k", Chat: "https://opencode.ai/zen/v1", Decide: "https://opencode.ai/zen/v1"}
	h1, h2 := sign(zen, "https://opencode.ai/zen/v1/systemone"), sign(zen, "https://opencode.ai/zen/v1/systemone")
	if s := h1.Get("x-opencode-session"); !openCodeRealSession(s) || h1.Get("x-opencode-session-id") != s || h2.Get("x-opencode-session") == s {
		t.Errorf("decisions: %q %q", s, h2.Get("x-opencode-session"))
	}
	// a turn's session, named before signing, stays
	req, _ := http.NewRequest("POST", "https://opencode.ai/zen/v1/chat/completions", nil)
	OpenCodeClient(req.Header, "conversation-x", "")
	turn := req.Header.Get("x-opencode-session")
	if err := zen.Sign(context.Background(), req, Chat, nil); err != nil || req.Header.Get("x-opencode-session") != turn {
		t.Errorf("a turn's session became %q (%v)", req.Header.Get("x-opencode-session"), err)
	}
	mine := zen
	mine.Headers = map[string]string{"X-Opencode-Session": "ses_mine"}
	if s := sign(mine, "https://opencode.ai/zen/v1/systemone").Values("x-opencode-session"); len(s) != 1 || s[0] != "ses_mine" {
		t.Errorf("the user's own session: %q", s)
	}
	if h := mine.listHeadersAt("https://opencode.ai/zen/v1"); h["X-Opencode-Session"] != "ses_mine" || len(h) != 1 {
		t.Errorf("the user's own session in a list: %q", h)
	}
	other := Provider{ID: "other", Key: "k", Chat: "https://other.test/v1"}
	if h := sign(other, "https://other.test/v1/chat/completions"); h.Get("x-opencode-session") != "" || h.Get("x-opencode-session-id") != "" {
		t.Errorf("another vendor: %v", h)
	}
	if h := other.listHeadersAt("https://other.test/v1"); len(h) != 0 {
		t.Errorf("another vendor's list: %v", h)
	}
}
