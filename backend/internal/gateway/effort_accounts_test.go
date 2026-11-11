package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// freeRefusesHigh is the ChatGPT backend's 400 for a reasoning level the
// account's plan doesn't take.
const freeRefusesHigh = `{"error":{"message":"Unsupported value: 'high' is not supported with the 'gpt-6-luna' model. Supported values are: 'none', 'low', and 'medium'.","type":"invalid_request_error","param":"reasoning.effort","code":"unsupported_value"}}`

// effortAccounts signs Codex in to a Free ChatGPT account, first, with a
// Plus one saved beside it, against a fake ChatGPT backend: the Free one
// turns gpt-6-luna at high and above away. freeLevels are the levels the
// Free account's model list gives gpt-6-luna. It returns the accounts
// each request was tried on, in order.
func effortAccounts(t *testing.T, freeLevels []string, plusOut *bool) func() []string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	restingUntil.Lock()
	restingUntil.m = map[string]time.Time{}
	restingUntil.Unlock()
	effortRefusals.Lock()
	effortRefusals.m = map[string]effortRefusal{}
	effortRefusals.Unlock()
	claims := func(m map[string]any) string {
		b, _ := json.Marshal(m)
		return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
	}
	auth := func(email, acct, plan string) map[string]any {
		return map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{
			"id_token": claims(map[string]any{"email": email,
				"https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": plan}}),
			"access_token":  claims(map[string]any{"exp": time.Now().Add(time.Hour).Unix(), "who": acct}),
			"refresh_token": "r-" + acct, "account_id": acct}}
	}
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	os.WriteFile(filepath.Join(home, ".codex", "auth.json"), mustJSON(auth("free@example.com", "acct-free", "free")), 0o600)
	os.MkdirAll(filepath.Dir(provider.Path()), 0o755)
	os.WriteFile(filepath.Join(filepath.Dir(provider.Path()), "logins.json"), mustJSON([]map[string]any{
		{"agent": "codex", "user": "plus@example.com", "plan": "plus", "on": true, "seen": time.Now(), "auth": auth("plus@example.com", "acct-plus", "plus")},
	}), 0o600)

	all := []string{"none", "low", "medium", "high", "xhigh", "max"}
	levels := func(ls []string) string {
		var out []string
		for _, l := range ls {
			out = append(out, `{"effort":"`+l+`"}`)
		}
		return "[" + strings.Join(out, ",") + "]"
	}
	var mu sync.Mutex
	var tried []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		acct := r.Header.Get("chatgpt-account-id")
		if strings.HasSuffix(r.URL.Path, "/models") {
			ls := all
			if acct == "acct-free" {
				ls = freeLevels
			}
			io.WriteString(w, `{"models":[{"slug":"gpt-6-luna","visibility":"list","supported_reasoning_levels":`+levels(ls)+`}]}`)
			return
		}
		var q struct {
			Reasoning struct {
				Effort string `json:"effort"`
			} `json:"reasoning"`
		}
		json.Unmarshal(b, &q)
		mu.Lock()
		tried = append(tried, acct+":"+q.Reasoning.Effort)
		mu.Unlock()
		switch {
		case acct == "acct-free" && strings.Contains(string(b), "make it fail"):
			w.WriteHeader(400)
			io.WriteString(w, `{"error":{"message":"Invalid 'input[0].content': string too long.","type":"invalid_request_error","param":"input","code":"string_above_max_length"}}`)
			return
		case acct == "acct-free" && q.Reasoning.Effort != "none" && q.Reasoning.Effort != "low" && q.Reasoning.Effort != "medium":
			w.WriteHeader(400)
			io.WriteString(w, strings.ReplaceAll(freeRefusesHigh, "'high'", "'"+q.Reasoning.Effort+"'"))
			return
		case acct == "acct-plus" && plusOut != nil && *plusOut:
			w.WriteHeader(429)
			io.WriteString(w, `{"error":{"message":"You've hit your usage limit"}}`)
			return
		}
		io.WriteString(w, sse(
			`data: {"type":"response.created","response":{"id":"r1","model":"gpt-6-luna"}}`,
			`data: {"type":"response.output_text.delta","delta":"pong"}`,
			`data: {"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":7,"output_tokens":1}}}`))
	}))
	t.Cleanup(up.Close)
	old := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = old })

	p, err := provider.Find("codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := tried
		tried = nil
		return out
	}
}

// #520: the Free account first, gpt-6-luna at high goes on to the Plus one
// when the Free one turns the level away, and after that straight to the
// Plus one; at medium the Free one answers. A request at fault otherwise
// isn't sent on.
func TestEffortRefusedGoesToAnotherAccount(t *testing.T) {
	all := []string{"none", "low", "medium", "high", "xhigh", "max"}
	tried := effortAccounts(t, all, nil)

	code, body := post(t, "/v1/responses", `{"model":"codex/gpt-6-luna","input":"ping","reasoning":{"effort":"high"}}`)
	if got := strings.Join(tried(), ","); code != 200 || !strings.Contains(body, "pong") || got != "acct-free:high,acct-plus:high" {
		t.Fatalf("high: status %d, tried %s: %s", code, got, body)
	}
	if !takesEffort(candidate{p: provider.Provider{ID: "codex", Account: &provider.Account{Agent: "codex", User: "free@example.com"}}, model: "gpt-6-luna"}, "medium") {
		t.Fatal("the Free account's refusal of high took medium off it too")
	}
	// the Free one isn't asked again for high, nor set aside for the others
	code, body = post(t, "/v1/responses", `{"model":"codex/gpt-6-luna","input":"ping","reasoning":{"effort":"high"}}`)
	if got := strings.Join(tried(), ","); code != 200 || got != "acct-plus:high" {
		t.Fatalf("high again: status %d, tried %s: %s", code, got, body)
	}
	code, body = post(t, "/v1/responses", `{"model":"codex/gpt-6-luna","input":"ping","reasoning":{"effort":"xhigh"}}`)
	if got := strings.Join(tried(), ","); code != 200 || got != "acct-plus:xhigh" {
		t.Fatalf("xhigh: status %d, tried %s: %s", code, got, body)
	}
	code, body = post(t, "/v1/responses", `{"model":"codex/gpt-6-luna","input":"ping","reasoning":{"effort":"medium"}}`)
	if got := strings.Join(tried(), ","); code != 200 || got != "acct-free:medium" {
		t.Fatalf("medium: status %d, tried %s: %s", code, got, body)
	}
	// a request at fault in itself is the agent's to hear, not sent on
	code, body = post(t, "/v1/responses", `{"model":"codex/gpt-6-luna","input":"make it fail","reasoning":{"effort":"medium"}}`)
	if got := strings.Join(tried(), ","); code != 400 || got != "acct-free:medium" {
		t.Fatalf("bad request: status %d, tried %s: %s", code, got, body)
	}
}

// The Free account's own model list gives gpt-6-luna no high: a request at
// high goes to the Plus one without the Free one being asked; with the
// Plus one out, the Free one is asked last, and its refusal is told as
// what it is, not lowered to medium behind the agent's back.
func TestEffortTheAccountListsLacks(t *testing.T) {
	plusOut := false
	tried := effortAccounts(t, []string{"none", "low", "medium"}, &plusOut)

	code, body := post(t, "/v1/responses", `{"model":"codex/gpt-6-luna","input":"ping","reasoning":{"effort":"high"}}`)
	if got := strings.Join(tried(), ","); code != 200 || got != "acct-plus:high" {
		t.Fatalf("high: status %d, tried %s: %s", code, got, body)
	}
	code, body = post(t, "/v1/responses", `{"model":"codex/gpt-6-luna","input":"ping","reasoning":{"effort":"low"}}`)
	if got := strings.Join(tried(), ","); code != 200 || got != "acct-free:low" {
		t.Fatalf("low: status %d, tried %s: %s", code, got, body)
	}
	plusOut = true
	code, body = post(t, "/v1/responses", `{"model":"codex/gpt-6-luna","input":"ping","reasoning":{"effort":"high"}}`)
	if got := strings.Join(tried(), ","); code != 400 || got != "acct-plus:high,acct-free:high" ||
		!strings.Contains(body, `doesn't take reasoning effort \"high\" on gpt-6-luna`) || !strings.Contains(body, "none, low, medium") {
		t.Fatalf("plus out: status %d, tried %s: %s", code, got, body)
	}
}

// Only a refusal of the reasoning level sent is one: another field's
// value, or another level, isn't.
func TestEffortRefusedWords(t *testing.T) {
	if takes, ok := effortRefused([]byte(freeRefusesHigh), "high"); !ok || strings.Join(takes, ",") != "none,low,medium" {
		t.Fatalf("not read: %v %v", takes, ok)
	}
	for _, c := range []struct{ body, sent string }{
		{freeRefusesHigh, "medium"},
		{`{"error":{"message":"Unsupported value: 'high' is not supported with this model.","param":"text.verbosity","code":"unsupported_value"}}`, "high"},
		{`{"error":{"message":"Unsupported value: 'priority' is not supported with this model.","param":"service_tier","code":"unsupported_value"}}`, "high"},
		{`{"error":{"message":"Invalid 'input[0].content': string too long."}}`, "high"},
		{freeRefusesHigh, ""},
	} {
		if _, ok := effortRefused([]byte(c.body), c.sent); ok {
			t.Errorf("read as a refusal of %q: %s", c.sent, c.body)
		}
	}
	// as a stream's error says it, the message alone
	if takes, ok := effortRefused([]byte("Codex: Unsupported value: 'xhigh' is not supported with the 'gpt-6-luna' model. Supported values are: 'low' and 'medium'."), "xhigh"); !ok || strings.Join(takes, ",") != "low,medium" {
		t.Errorf("message alone not read: %v %v", takes, ok)
	}
	if _, ok := effortRefused([]byte(`{"detail":"reasoning.effort 'high' is not available on your plan"}`), "high"); !ok {
		t.Error("a refusal naming reasoning.effort not read")
	}
}
