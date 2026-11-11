package gateway

import (
	"io"
	"net/http"

	"strings"
	"sync"
	"testing"
	"time"
)

// clsUp is a Chat vendor that classifies: it answers what answer says,
// after wait, and keeps what it was asked.
type clsUp struct {
	mu     sync.Mutex
	answer string
	fail   int
	wait   time.Duration
	asked  []string
	agents []string
}

func (u *clsUp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	u.mu.Lock()
	u.asked = append(u.asked, string(b))
	answer, fail, wait := u.answer, u.fail, u.wait
	u.mu.Unlock()
	if wait > 0 {
		select {
		case <-time.After(wait):
		case <-r.Context().Done():
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if fail != 0 {
		w.WriteHeader(fail)
		io.WriteString(w, `{"error":{"message":"classifier down"}}`)
		return
	}
	io.WriteString(w, `{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":`+quote(answer)+`},"finish_reason":"stop"}],"usage":{"prompt_tokens":50,"completion_tokens":1}}`)
}

func (u *clsUp) n() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.asked)
}

func (u *clsUp) last() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.asked[len(u.asked)-1]
}

func (u *clsUp) set(answer string, fail int) {
	u.mu.Lock()
	u.answer, u.fail = answer, fail
	u.mu.Unlock()
}

func postOK(t *testing.T, s *Server, session, body string) (string, Route) {
	t.Helper()
	code, out := postAs(t, s, session, body)
	if code != 200 {
		t.Fatalf("%d %s", code, out)
	}
	return out, lastRoute(s)
}

func TestUserText(t *testing.T) {
	req := &Request{Messages: []Message{
		{Role: "user", Parts: []Part{{Kind: Text, Text: "first"}}},
		{Role: "assistant", Parts: []Part{{Kind: Text, Text: "ok"}}},
		{Role: "user", Parts: []Part{{Kind: Text, Text: "<system-reminder>\nx\n</system-reminder>"}, {Kind: Text, Text: "  second  "}}},
	}}
	if got := userText(req); got != "second" {
		t.Fatalf("%q", got)
	}
	big := strings.Repeat("a", 3000) + strings.Repeat("m", 5000) + strings.Repeat("z", 1000)
	got := userText(&Request{Messages: []Message{{Role: "user", Parts: []Part{{Kind: Text, Text: big}}}}})
	if !strings.HasPrefix(got, strings.Repeat("a", 3000)+"\n…\n") || !strings.HasSuffix(got, strings.Repeat("z", 1000)) || strings.Contains(got, "m") {
		t.Fatalf("%d", len(got))
	}
	for in, want := range map[string]string{"1": "x", "2": "y", " 0 ": "", "2\n": "y"} {
		got, said, err := readIntent(in, []string{"x", "y"})
		if err != nil || got != want {
			t.Fatalf("%q: %q %v", in, got, err)
		}
		if said != strings.TrimSpace(in) {
			t.Fatalf("%q: said %q", in, said)
		}
	}
	for _, in := range []string{"3", "-", "none", ""} {
		if _, _, err := readIntent(in, []string{"x", "y"}); err == nil {
			t.Fatalf("%q read", in)
		}
	}
}
