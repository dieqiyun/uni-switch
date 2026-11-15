package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// heardHeaders' sibling for a fake vendor: the headers of each request it
// was sent.
type sentTo struct {
	mu   sync.Mutex
	seen []http.Header
}

func (s *sentTo) last() http.Header {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.seen) == 0 {
		return nil
	}
	return s.seen[len(s.seen)-1]
}

func (s *sentTo) answer(vendor provider.Protocol, status int) http.HandlerFunc {
	reply := vendorReply(vendor, status, "x")
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.seen = append(s.seen, r.Header.Clone())
		s.mu.Unlock()
		reply(w, r)
	}
}

// the agent's request, carrying a header to pass, one to pass that also
// has a fixed value, uni-switch's key in every place an agent puts it, and a
// header nobody lists
func passRequest(path, body string) *http.Request {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("X-Tenant", "tenant-from-client")
	r.Header.Set("X-Route", "route-from-client")
	r.Header.Set("X-Trace-Token", "trace-secret-0123456789")
	r.Header.Set("X-Unlisted", "no")
	r.Header.Set("Authorization", "Bearer uni-switch-gateway-key")
	r.Header.Set("X-Api-Key", "uni-switch-gateway-key")
	return r
}

// 3jRzfF52Qx on X: a header the provider lists goes to it as the agent
// sent it, on every API (relayed, translated, count_tokens), standing over
// the provider's fixed Header of the same name; the fixed one goes when
// the agent sends none; uni-switch's key never goes.
func TestPassHeadersReachTheProvider(t *testing.T) {
	for _, c := range []struct {
		name, path, body string
		vendor           provider.Protocol
	}{
		{"messages relayed", "/v1/messages", `{"model":"fake/sol","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`, provider.Anthropic},
		{"messages to chat", "/v1/messages", `{"model":"fake/sol","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`, provider.Chat},
		{"chat", "/v1/chat/completions", `{"model":"fake/sol","messages":[{"role":"user","content":"hi"}]}`, provider.Chat},
		{"chat stream", "/v1/chat/completions", `{"model":"fake/sol","stream":true,"messages":[{"role":"user","content":"hi"}]}`, provider.Chat},
		{"responses to chat", "/v1/responses", `{"model":"fake/sol","input":"hi"}`, provider.Chat},
		{"count_tokens", "/v1/messages/count_tokens", `{"model":"fake/sol","messages":[{"role":"user","content":"hi"}]}`, provider.Anthropic},
	} {
		t.Run(c.name, func(t *testing.T) {
			fresh(t)
			up := &sentTo{}
			p := provider.Provider{ID: "fake", Name: "Fake", Key: "sk-upstream-0123456789", Models: []string{"sol"},
				Headers:     map[string]string{"X-Route": "route-fixed", "X-Fixed": "fixed"},
				PassHeaders: []string{"x-tenant", "X-ROUTE", "x-trace-token", "x-missing"}}
			rhVendor(t, p, c.vendor, up.answer(c.vendor, 200))
			s := New()
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, passRequest(c.path, c.body))
			if rec.Code != 200 {
				t.Fatalf("%d %s", rec.Code, rec.Body)
			}
			h := up.last()
			if h == nil {
				t.Fatal("the vendor was sent nothing")
			}
			for k, want := range map[string]string{
				"X-Tenant":      "tenant-from-client",
				"X-Route":       "route-from-client", // the client's over the fixed one
				"X-Trace-Token": "trace-secret-0123456789",
				"X-Fixed":       "fixed",
			} {
				if got := h.Values(k); len(got) != 1 || got[0] != want {
					t.Errorf("%s = %q, want %q", k, got, want)
				}
			}
			for _, k := range []string{"X-Unlisted", "X-Missing"} {
				if got := h.Get(k); got != "" {
					t.Errorf("%s = %q sent", k, got)
				}
			}
			if a := h.Get("Authorization") + h.Get("X-Api-Key"); strings.Contains(a, "uni-switch-gateway-key") || !strings.Contains(a, "sk-upstream-0123456789") {
				t.Errorf("the vendor's key headers %q, %q", h.Get("Authorization"), h.Get("X-Api-Key"))
			}
		})
	}
}
