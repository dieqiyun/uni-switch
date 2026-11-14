package gateway

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/netproxy"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// #2651 (demystify0137): a Claude account behind a socks5:// proxy was
// answered 502 after minutes, Claude Code failing every try as
// UnsupportedProxyProtocol, since Bun, which Claude Code is built with,
// takes only http:// and https:// proxies. Every Claude Code uni-switch runs
// for the subscription — a turn, a model test, /usage — is given uni-switch's
// HTTP bridge to the SOCKS proxy instead, never socks5:// itself.
func TestClaudeCodeGivenTheBridgeForASOCKSProxy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for Claude Code")
	}
	const socks = "socks5://127.0.0.1:1"
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	// a turn's run answers with its proxy; a test's or /usage's writes it
	// to log
	script := `#!/bin/sh
proxies="https=$HTTPS_PROXY http=$HTTP_PROXY all=$ALL_PROXY lower=$https_proxy"
case " $* " in
*" stream-json "*)
  while read -r line; do
    echo '{"type":"stream_event","event":{"type":"message_start","message":{"id":"m","model":"claude-sonnet-5","usage":{"input_tokens":1}}}}'
    echo '{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"'"$proxies"'"}}}'
    echo '{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}}'
    echo '{"type":"stream_event","event":{"type":"message_stop"}}'
    echo '{"type":"result","subtype":"success","is_error":false,"result":""}'
  done ;;
*)
  echo "$proxies" > ` + log + `
  echo '{"type":"result","is_error":false,"result":"Hi!"}' ;;
esac
`
	testenv.Program(t, filepath.Join(dir, "claude"), script)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	runsEndWithTest(t, dir)

	bridged := func(what, got string) {
		t.Helper()
		if strings.Contains(got, "socks5") {
			t.Fatalf("%s: Claude Code was given the SOCKS proxy: %s", what, got)
		}
		for _, k := range []string{"https", "http", "all", "lower"} {
			if !strings.Contains(got, k+"=http://127.0.0.1:") {
				t.Fatalf("%s: no bridge in %s: %s", what, k, got)
			}
		}
	}

	s := New()
	t.Cleanup(s.subscription.abortAll)
	p := provider.Provider{ID: "claude", Proxy: socks, Account: &provider.Account{Agent: "claude", User: "u"}}
	body := `{"model":"claude-sonnet-5","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`
	rec := httptest.NewRecorder()
	var u Usage
	if code, msg := s.serveClaudeSubscription(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)), provider.Anthropic, p, "claude-sonnet-5", []byte(body), &u); code != 200 {
		t.Fatalf("%d %s", code, msg)
	}
	var res struct {
		Content []struct{ Text string } `json:"content"`
	}
	json.Unmarshal(rec.Body.Bytes(), &res)
	if len(res.Content) == 0 {
		t.Fatalf("no answer: %s", rec.Body)
	}
	bridged("a turn", res.Content[0].Text)

	ctx := netproxy.With(context.Background(), socks)
	if err := askClaude(ctx, "", "haiku"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	bridged("a model test", string(b))
	os.Remove(log)
	if _, err := claudeUsage(ctx, ""); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(log)
	bridged("/usage", string(b))
}
