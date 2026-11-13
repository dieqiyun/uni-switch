package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #933: a ChatGPT plan signed in with OpenAI's Sign in with ChatGPT is
// asked on api.openai.com's Responses with its token, whichever API the
// agent speaks, with the agent's own instructions — none of Codex's — and
// without the fields that API refuses for a ChatGPT token.
func TestChatGPTAPIRequests(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))

	f := &fake{t: t, reply: sse(
		`data: {"type":"response.created","response":{"id":"r1","model":"gpt-5.5"}}`,
		`data: {"type":"response.output_text.delta","delta":"pong"}`,
		`data: {"type":"response.completed","response":{"id":"r1","status":"completed","usage":{"input_tokens":7,"output_tokens":1}}}`)}
	up := httptest.NewServer(f)
	t.Cleanup(up.Close)
	t.Cleanup(provider.SIWCForTest(up.URL, up.URL+"/v1"))

	auth, _ := json.Marshal(map[string]any{
		"clientId": "oaiapp_1", "hostId": "urn:uuid:00000000-0000-4000-8000-000000000000", "sub": "user-1",
		"email": "me@example.com", "access": "at-live", "refresh": "rt-live",
		"expires": time.Now().Add(time.Hour), "scopes": []string{"chatgpt.tokens.use.direct"},
	})
	dir := filepath.Dir(provider.Path())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal([]map[string]any{{
		"agent": provider.ChatGPTAPIID, "user": "me@example.com", "plan": "plus", "on": true, "auth": json.RawMessage(auth),
	}})
	if err := os.WriteFile(filepath.Join(dir, "logins.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct{ name, path, body string }{
		{"chat", "/v1/chat/completions", `{"model":"chatgpt-api/gpt-5.5","max_tokens":64,"temperature":0.3,"messages":[{"role":"system","content":"You are my agent."},{"role":"user","content":"ping"}]}`},
		{"messages", "/v1/messages", `{"model":"chatgpt-api/gpt-5.5","max_tokens":64,"temperature":0.3,"system":"You are my agent.","messages":[{"role":"user","content":"ping"}]}`},
		{"responses", "/v1/responses", `{"model":"chatgpt-api/gpt-5.5","max_output_tokens":64,"temperature":0.3,"store":true,"instructions":"You are my agent.","input":"ping"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f.got, f.path, f.head = nil, "", nil
			code, out := post(t, tt.path, tt.body)
			if code != 200 || !strings.Contains(out, "pong") {
				t.Fatalf("%d %s", code, out)
			}
			if f.path != "/v1/responses" {
				t.Fatalf("went to %s", f.path)
			}
			if got := f.head.Get("Authorization"); got != "Bearer at-live" {
				t.Errorf("Authorization %q", got)
			}
			if f.head.Get("chatgpt-account-id") != "" || f.head.Get("originator") != "" {
				t.Errorf("Codex's headers sent: %v", f.head)
			}
			var m map[string]any
			if err := json.Unmarshal(f.got, &m); err != nil {
				t.Fatalf("upstream %s", f.got)
			}
			if m["model"] != "gpt-5.5" || m["store"] != false || m["stream"] != true {
				t.Errorf("body %s", f.got)
			}
			for _, k := range []string{"max_output_tokens", "temperature", "prompt_cache_retention"} {
				if _, ok := m[k]; ok {
					t.Errorf("%s sent: %s", k, f.got)
				}
			}
			body := string(f.got)
			if !strings.Contains(body, "You are my agent.") || strings.Contains(body, "Codex") {
				t.Errorf("instructions not the agent's own: %s", body)
			}
		})
	}
}
