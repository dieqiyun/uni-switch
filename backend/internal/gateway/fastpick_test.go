package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// postAs sends body to path with agent's own token, as the agent wired to
// uni-switch does.
func postFrom(t *testing.T, s *Server, agent, path, body string) Route {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+TokenFor(agent))
	if strings.HasSuffix(path, "/messages") {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
	}
	return lastRoute(s)
}

// A model switched fast in an agent's picker (#954) is asked for in its
// vendor's fast mode on that agent's requests, whichever API it speaks, and
// the trace says so; another agent's requests for it, and the agent's for
// the same model at another provider, go as they are.
func TestFastPick(t *testing.T) {
	s, up := fasted(t)
	if err := provider.SetFastPick("codex", "oa/gpt-6.1-sol", true); err != nil {
		t.Fatal(err)
	}
	asks := []struct{ name, path, body string }{
		{"chat", "/v1/chat/completions", `{"model":"oa/gpt-6.1-sol","messages":[{"role":"user","content":"hi"}]}`},
		{"responses", "/v1/responses", `{"model":"oa/gpt-6.1-sol","input":"hi"}`},
		{"anthropic", "/v1/messages", `{"model":"oa/gpt-6.1-sol","max_tokens":1024,"messages":[{"role":"user","content":"hi"}]}`},
	}
	for _, a := range asks {
		r := postFrom(t, s, "codex", a.path, a.body)
		host, path, _, body := up.last(t)
		if host != "api.openai.com" || body["service_tier"] != "priority" || body["model"] != "gpt-6.1-sol" {
			t.Fatalf("%s: sent to %s%s %v", a.name, host, path, body)
		}
		if len(r.Tries) != 1 || !r.Tries[0].Fast || len(r.Order) == 0 || !r.Order[0].Fast {
			t.Fatalf("%s: traced %+v %+v", a.name, r.Tries, r.Order)
		}

		r = postFrom(t, s, "opencode", a.path, a.body)
		if _, _, _, body := up.last(t); body["service_tier"] != nil || r.Tries[0].Fast {
			t.Fatalf("%s: another agent's request went fast: %v %+v", a.name, body, r.Tries)
		}
	}
	postFrom(t, s, "codex", "/v1/chat/completions", `{"model":"oa/gpt-6.1-mini","messages":[{"role":"user","content":"hi"}]}`)
	if _, _, _, body := up.last(t); body["service_tier"] != nil {
		t.Fatalf("a model not switched fast went fast: %v", body)
	}
	postFrom(t, s, "codex", "/v1/chat/completions", `{"model":"rl/gpt-6.1-sol","messages":[{"role":"user","content":"hi"}]}`)
	if host, _, _, body := up.last(t); host != "relay.example" || body["service_tier"] != nil {
		t.Fatalf("the same model at a relay went fast: %s %v", host, body)
	}

	// switched back: as it was
	if err := provider.SetFastPick("codex", "oa/gpt-6.1-sol", false); err != nil {
		t.Fatal(err)
	}
	postFrom(t, s, "codex", "/v1/responses", `{"model":"oa/gpt-6.1-sol","input":"hi"}`)
	if _, _, _, body := up.last(t); body["service_tier"] != nil {
		t.Fatalf("switched back, still fast: %v", body)
	}
}

// A GPT model on a provider the user added by its address, switched fast in
// the picker (#1615), is asked for priority there, whichever API the agent
// speaks.
func TestFastPickOwnProvider(t *testing.T) {
	s, up := fasted(t)
	if err := provider.SetFastPick("claude", "rl/gpt-6.1-sol", true); err != nil {
		t.Fatal(err)
	}
	for _, a := range []struct{ path, body string }{
		{"/v1/messages", `{"model":"rl/gpt-6.1-sol","max_tokens":1024,"messages":[{"role":"user","content":"hi"}]}`},
		{"/v1/chat/completions", `{"model":"rl/gpt-6.1-sol","messages":[{"role":"user","content":"hi"}]}`},
	} {
		r := postFrom(t, s, "claude", a.path, a.body)
		host, path, _, body := up.last(t)
		if host != "relay.example" || body["service_tier"] != "priority" || body["speed"] != nil || !r.Tries[0].Fast {
			t.Fatalf("%s: sent to %s%s %v, traced %+v", a.path, host, path, body, r.Tries)
		}
	}
}
