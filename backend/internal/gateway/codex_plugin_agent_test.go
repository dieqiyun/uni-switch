package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// OpenAI's Codex plugin for Claude Code (/codex:rescue, /codex:review) runs
// `codex app-server` under the name "Claude Code", and Codex 0.162.1 sends
// that as its originator and User-Agent. Its calls are Codex's, not Claude
// Code's (wyztony on X: "claude通过/codex skill时，用量不会记录codex的记录").
// The headers are the ones captured from codex-companion.mjs through a
// sandbox gateway. Claude Code itself, a "Claude Code" name without Codex's
// turn headers, and an app that runs Codex as its own engine (Lody) keep
// the agent they had.
func TestCodexPluginForClaudeCodeIsCodex(t *testing.T) {
	setHome(t, t.TempDir())
	f := &fake{t: t, reply: sse(
		`data: {"type":"response.created","response":{"id":"r1","model":"m1"}}`,
		`data: {"type":"response.completed","response":{"id":"r1","output":[],"usage":{"input_tokens":5,"output_tokens":2}}}`)}
	setup(t, provider.Responses, f)
	const plugin = "Claude Code/0.162.1 (Mac OS 26.2.0; arm64) dumb (Claude Code; 1.0.6)"
	turn := map[string]string{
		"X-Codex-Window-Id":     "019a0f7e-0000-7000-8000-000000000001:0",
		"X-Codex-Turn-Metadata": `{"turn_id":"019a0f7e-0000-7000-8000-000000000002","sandbox":"none"}`,
		"X-Codex-Beta-Features": "remote_compaction_v2",
		"Session-Id":            "019a0f7e-0000-7000-8000-000000000001",
		"Thread-Id":             "019a0f7e-0000-7000-8000-000000000001",
	}
	for i, c := range []struct {
		name, ua, originator string
		codexTurn            bool
		want                 string
	}{
		{"plugin task", plugin, "Claude Code", true, "codex"},
		{"plugin, originator only", "", "Claude Code", true, "codex"},
		{"claude code itself", "claude-cli/2.1.0 (external, cli)", "", false, usage.AgentOf("claude-cli/2.1.0 (external, cli)")},
		{"the name without codex's turn", plugin, "", false, usage.AgentOf(plugin)},
		{"lody runs codex as its engine", "lody/1.2.0", "lody", true, usage.AgentOf("lody/1.2.0")},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"fake/m1","stream":true,"input":"hi"}`))
		req.Header.Set("Authorization", "Bearer "+Token)
		req.Header.Set("User-Agent", c.ua)
		if c.originator != "" {
			req.Header.Set("Originator", c.originator)
		}
		if c.codexTurn {
			for k, v := range turn {
				req.Header.Set(k, v)
			}
		}
		New().Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", c.name, rec.Code, rec.Body.String())
		}
		var got []usage.Record
		for range 100 {
			if got = usage.Load(time.Time{}); len(got) > i {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if len(got) <= i || got[i].Agent != c.want {
			var agent string
			if len(got) > i {
				agent = got[i].Agent
			}
			t.Fatalf("%s: recorded as %q, want %q", c.name, agent, c.want)
		}
	}
}
