package gateway

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/claudecode"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// A machine with no Claude Code of its own (a server, a container) answers
// a Claude subscription's request with the one `uni-switch claude-code install`
// downloaded into uni-switch's cache (Jorben on Discord): before, the request
// failed "Claude Code is not installed" with that build right there.
func TestClaudeSubscriptionRunsTheDownloadedClaudeCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for Claude Code")
	}
	t.Setenv("PATH", t.TempDir()) // no claude of the machine's own
	exe := filepath.Join(claudecode.Root(), "2.1.296", "claude")
	t.Cleanup(func() { _ = claudecode.Remove() })
	testenv.Program(t, exe, `#!/bin/sh
read -r line
echo '{"type":"stream_event","event":{"type":"message_start","message":{"id":"m","model":"claude-sonnet-5","usage":{"input_tokens":1}}}}'
echo '{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}'
echo '{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"from the download"}}}'
echo '{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}}'
echo '{"type":"stream_event","event":{"type":"message_stop"}}'
`)
	if c, err := claudeBinary(); err != nil || c.path != exe {
		t.Fatalf("claudeBinary() = %+v, %v; want %s", c, err, exe)
	}
	s := New()
	t.Cleanup(s.subscription.abortAll)
	p := provider.Provider{ID: "claude", Account: &provider.Account{Agent: "claude", User: "u"}}
	body := `{"model":"claude-sonnet-5","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hi"}]}`
	rec := httptest.NewRecorder()
	var u Usage
	s.serveClaudeSubscription(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)), provider.Anthropic, p, "claude-sonnet-5", []byte(body), &u)
	if out := rec.Body.String(); !strings.Contains(out, "from the download") {
		t.Fatalf("reply: %q", out)
	}
}

// With neither, the error says how to have uni-switch download it.
func TestClaudeSubscriptionWithoutClaudeCodeSaysHowToGetIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("WSL may have one")
	}
	t.Setenv("PATH", t.TempDir())
	_ = os.RemoveAll(claudecode.Root())
	_, err := claudeBinary()
	if err == nil || !strings.Contains(err.Error(), "uni-switch claude-code install") {
		t.Fatalf("claudeBinary() error = %v", err)
	}
}
