package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #3491 (laomao04_65777 on Discord): Zen's free models answered 429 "Free
// usage limit reached. Free models work in OpenCode" to uni-switch while real
// OpenCode on the same machine was served. A free model is now asked as
// OpenCode 2.2.5 asked it (../provider/testdata/opencode-2.2.5-chat.json),
// on every protocol: at its v2 path, with only the headers it sends (none
// of the agent's own), its environment block for the folder the agent says
// it works in, and that folder's project id.
func TestZenFreeAsOpenCode225(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var capture struct {
		Headers [][2]string `json:"headers"`
	}
	b, err := os.ReadFile("../provider/testdata/opencode-2.2.5-chat.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &capture); err != nil {
		t.Fatal(err)
	}
	transport := map[string]bool{"host": true, "content-length": true, "accept-encoding": true, "connection": true}
	var want []string
	for _, kv := range capture.Headers {
		if !transport[strings.ToLower(kv[0])] {
			want = append(want, strings.ToLower(kv[0]))
		}
	}
	slices.Sort(want)

	if err := provider.Save(provider.Provider{ID: "zen", Name: "Zen", Key: "public",
		Chat: "https://opencode.ai/zen/v1", Responses: "https://opencode.ai/zen/v1", Anthropic: "https://opencode.ai/zen",
		Models: []string{"mimo-v2.6-flash-free"}}); err != nil {
		t.Fatal(err)
	}
	type seen struct {
		path string
		h    http.Header
		body map[string]any
	}
	var got []seen
	s := New()
	s.client = &http.Client{Transport: countTransport(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		got = append(got, seen{r.URL.Host + r.URL.Path, r.Header.Clone(), m})
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(chatSSE("hi", "", "")))}, nil
	})}
	dir := filepath.Join(t.TempDir(), "proj")
	os.MkdirAll(dir, 0o755)
	dir, _ = filepath.EvalSymlinks(dir)
	// as Claude Code says it, on its own line
	dirJSON, _ := json.Marshal("\n - Primary working directory: " + dir + "\n")
	dirOnly, _ := json.Marshal(dir)
	claudeCode := func(r *http.Request) {
		r.Header.Set("User-Agent", "claude-cli/2.1.0 (external, cli)")
		r.Header.Set("anthropic-version", "2023-06-01")
		r.Header.Set("x-app", "cli")
		r.Header.Set("X-Stainless-Lang", "js")
	}
	codex := func(r *http.Request) {
		r.Header.Set("User-Agent", "codex_cli_rs/0.200.0")
		r.Header.Set("originator", "codex_cli_rs")
		r.Header.Set("x-codex-turn-metadata", "{}")
	}
	for _, c := range []struct {
		name, path, body string
		as               func(*http.Request)
	}{
		{"chat", "/v1/chat/completions", `{"model":"zen/mimo-v2.6-flash-free","messages":[{"role":"system","content":"You help. ` + strings.Trim(string(dirJSON), `"`) + `"},{"role":"user","content":"hi"}]}`, nil},
		{"messages", "/v1/messages", `{"model":"zen/mimo-v2.6-flash-free","max_tokens":10,"system":"You are Claude Code. ` + strings.Trim(string(dirJSON), `"`) + `","messages":[{"role":"user","content":"hi"}]}`, claudeCode},
		{"responses", "/v1/responses", `{"model":"zen/mimo-v2.6-flash-free","instructions":"You are Codex.","input":[{"role":"user","content":"<environment_context><cwd>` + strings.Trim(string(dirOnly), `"`) + `</cwd></environment_context>"}]}`, codex},
	} {
		got = nil
		req := httptest.NewRequest("POST", c.path, strings.NewReader(c.body))
		req.Header.Set("Content-Type", "application/json")
		if c.as != nil {
			c.as(req)
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 || len(got) != 1 {
			t.Fatalf("%s: %d %s (%d sent)", c.name, rec.Code, rec.Body, len(got))
		}
		r := got[0]
		// the zen provider here is Chat-first; whatever protocol the
		// agent spoke, the free request goes at OpenCode 2's path
		if !strings.HasPrefix(r.path, "opencode.ai/inference/openai/v1/") && !strings.HasPrefix(r.path, "opencode.ai/inference/anthropic/v1/") {
			t.Errorf("%s: asked at %s", c.name, r.path)
		}
		var names []string
		for k := range r.h {
			names = append(names, strings.ToLower(k))
		}
		slices.Sort(names)
		// Messages carries anthropic-version and x-api-key besides, as
		// OpenCode's Anthropic requests do
		names = slices.DeleteFunc(names, func(k string) bool { return k == "anthropic-version" || k == "x-api-key" })
		if !slices.Equal(names, want) {
			t.Errorf("%s: headers\n got %v\nwant %v", c.name, names, want)
		}
		if r.h.Get("User-Agent") != provider.OpenCodeUserAgent() || r.h.Get("Accept") != "*/*" {
			t.Errorf("%s: User-Agent %q Accept %q", c.name, r.h.Get("User-Agent"), r.h.Get("Accept"))
		}
		if p := r.h.Get("x-opencode-project"); p != provider.OpenCodeProject(dir) {
			t.Errorf("%s: project %q, want %q (%s's)", c.name, p, provider.OpenCodeProject(dir), dir)
		}
		sys := systemText(r.body)
		if !strings.Contains(sys, provider.OpenCodeEnvironment(dir)) || strings.Count(sys, "<env>") != 1 {
			t.Errorf("%s: system prompt %q", c.name, sys)
		}
	}

	// OpenCode's own request through uni-switch: its block and project as it
	// sent them, nothing added
	got = nil
	own := provider.OpenCodeEnvironment("/somewhere/else")
	ownJSON, _ := json.Marshal("You are opencode.\n" + own)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"zen/mimo-v2.6-flash-free","messages":[{"role":"system","content":`+string(ownJSON)+`},{"role":"user","content":"hi"}]}`))
	req.Header.Set("x-opencode-project", "0123456789abcdef0123456789abcdef01234567")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 || len(got) != 1 {
		t.Fatalf("OpenCode's own: %d %s", rec.Code, rec.Body)
	}
	if sys := systemText(got[0].body); strings.Count(sys, "<env>") != 1 || !strings.Contains(sys, own) {
		t.Errorf("OpenCode's own block: %q", sys)
	}
	if p := got[0].h.Get("x-opencode-project"); p != "0123456789abcdef0123456789abcdef01234567" {
		t.Errorf("OpenCode's own project became %q", p)
	}
}

// systemText is a request body's system prompt, on any protocol.
func systemText(m map[string]any) string {
	var out []string
	add := func(v any) {
		switch v := v.(type) {
		case string:
			out = append(out, v)
		case []any:
			for _, p := range v {
				if pm, ok := p.(map[string]any); ok {
					if s, ok := pm["text"].(string); ok {
						out = append(out, s)
					}
				}
			}
		}
	}
	add(m["system"])
	add(m["instructions"])
	for _, key := range []string{"messages", "input"} {
		msgs, _ := m[key].([]any)
		for _, x := range msgs {
			mm, _ := x.(map[string]any)
			if r := mm["role"]; r == "system" || r == "developer" {
				add(mm["content"])
			}
		}
	}
	return strings.Join(out, "\n")
}
