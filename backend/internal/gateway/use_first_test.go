package gateway

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// quotaKeyed answers from the key each request came with, or, once out,
// with OpenAI's 429 insufficient_quota.
type quotaKeyed struct {
	mu    sync.Mutex
	tried []string
	out   bool
}

func (k *quotaKeyed) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.ReadAll(r.Body)
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	k.mu.Lock()
	k.tried = append(k.tried, key)
	out := k.out
	k.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if out {
		w.WriteHeader(429)
		io.WriteString(w, carriedQuotaBody)
		return
	}
	io.WriteString(w, `{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"from `+key+`"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`)
}

// Bare models follow provider order and fail over when a provider runs out.
// The retired use-first flag is inert and cannot reorder that list.
func TestUseFirstLeadsTheGroup(t *testing.T) {
	fresh(t)
	sub, api := &quotaKeyed{}, &quotaKeyed{}
	serveOn(t, "sub", "ksub", []string{"m"}, sub)
	serveOn(t, "api", "kapi", []string{"m"}, api)
	s := New()
	ask := func(n string) string {
		t.Helper()
		code, body := postAs(t, s, "", `{"model":"m","messages":[{"role":"user","content":"`+n+`"}]}`)
		if code != 200 {
			t.Fatalf("%s: %d %s", n, code, body)
		}
		return body
	}

	if body := ask("listed order"); !strings.Contains(body, "from ksub") {
		t.Fatalf("none set: %s, want the first listed", body)
	}
	if err := provider.SetUseFirst("api", true); err != nil {
		t.Fatal(err)
	}
	if body := ask("legacy use-first remains inert"); !strings.Contains(body, "from ksub") {
		t.Fatalf("provider order: %s", body)
	}
	sub.mu.Lock()
	sub.out = true
	sub.mu.Unlock()
	if body := ask("first provider out of credit"); !strings.Contains(body, "from kapi") {
		t.Fatalf("ordinary provider fallback: %s", body)
	}
}
