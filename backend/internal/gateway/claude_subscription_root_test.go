package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
	"github.com/dieqiyun/uni-switch/backend/internal/wslrun"
)

// claudeRootRefusal is what Claude Code 2.1.296 prints, and exits 1 on,
// when it is run as root with --dangerously-skip-permissions, IS_SANDBOX
// not "1" and CLAUDE_CODE_BUBBLEWRAP unset (FrierenF on Discord, uni-switch
// on a server as root).
const claudeRootRefusal = "--dangerously-skip-permissions cannot be used with root/sudo privileges for security reasons"

// fakeRootClaude refuses as Claude Code run as root does while the file
// it returns is there, and otherwise answers with the IS_SANDBOX it was
// given.
func fakeRootClaude(t *testing.T) (root string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for Claude Code")
	}
	dir := t.TempDir()
	root = filepath.Join(dir, "root")
	script := `#!/bin/sh
[ -e '` + root + `' ] && case " $* " in *" --dangerously-skip-permissions "*)
  if [ "$IS_SANDBOX" != 1 ] && [ -z "$CLAUDE_CODE_BUBBLEWRAP" ]; then
    echo '` + claudeRootRefusal + `' >&2; exit 1
  fi;;
esac
while read -r line; do
  echo '{"type":"stream_event","event":{"type":"message_start","message":{"id":"m","model":"claude-sonnet-5","usage":{"input_tokens":1}}}}'
  echo '{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"IS_SANDBOX='"${IS_SANDBOX-unset}"'"}}}'
  echo '{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}}'
  echo '{"type":"stream_event","event":{"type":"message_stop"}}'
  echo '{"type":"result","subtype":"success","is_error":false,"result":""}'
done
`
	testenv.Program(t, filepath.Join(dir, "claude"), script)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	runsEndWithTest(t, dir)
	return root
}

// Run as root, the Claude subscription answers: Claude Code is told
// IS_SANDBOX=1, which its root check lets --dangerously-skip-permissions
// through on. Run as anyone else, its env is what it was.
func TestClaudeRunsAsRoot(t *testing.T) {
	root := fakeRootClaude(t)
	// unset for the test; t.Setenv puts back what was there
	t.Setenv("IS_SANDBOX", "")
	os.Unsetenv("IS_SANDBOX")
	t.Setenv("CLAUDE_CODE_BUBBLEWRAP", "")
	os.Unsetenv("CLAUDE_CODE_BUBBLEWRAP")
	s := New()
	t.Cleanup(s.subscription.abortAll)
	p := provider.Provider{ID: "claude", Account: &provider.Account{Agent: "claude", User: "u"}}
	ask := func(say string) (int, string) {
		t.Helper()
		body := `{"model":"claude-sonnet-5","max_tokens":100,"messages":[{"role":"user","content":"` + say + `"}]}`
		rec := httptest.NewRecorder()
		var u Usage
		code, msg := s.serveClaudeSubscription(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)), provider.Anthropic, p, "claude-sonnet-5", []byte(body), &u)
		if code != 200 {
			return code, msg
		}
		var res struct {
			Content []struct{ Text string } `json:"content"`
		}
		json.Unmarshal(rec.Body.Bytes(), &res)
		if len(res.Content) == 0 {
			t.Fatalf("no answer: %s", rec.Body)
		}
		return code, res.Content[0].Text
	}

	euid := claudeEuid
	t.Cleanup(func() { claudeEuid = euid })
	claudeEuid = func() int { return 0 }
	if err := os.WriteFile(root, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, got := ask("as root"); code != 200 || got != "IS_SANDBOX=1" {
		t.Fatalf("as root: %d %q", code, got)
	}

	claudeEuid = func() int { return 1000 }
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if code, got := ask("as a user"); code != 200 || got != "IS_SANDBOX=unset" {
		t.Fatalf("as a user: %d %q", code, got)
	}
}

func TestClaudeAsRootEnv(t *testing.T) {
	euid := claudeEuid
	t.Cleanup(func() { claudeEuid = euid })
	local := claudeCLI{path: "/usr/bin/claude"}
	base := []string{"PATH=/bin", "HOME=/root"}

	claudeEuid = func() int { return 1000 }
	for _, env := range [][]string{base, append(slices.Clone(base), "IS_SANDBOX=yes")} {
		if got := local.asRoot(slices.Clone(env)); !slices.Equal(got, env) {
			t.Fatalf("not root: %q, want %q as it was", got, env)
		}
	}

	claudeEuid = func() int { return 0 }
	for _, c := range []struct{ env, want []string }{
		{base, append(slices.Clone(base), "IS_SANDBOX=1")},
		// a value Claude Code doesn't take is put right
		{[]string{"IS_SANDBOX=true", "PATH=/bin"}, []string{"PATH=/bin", "IS_SANDBOX=1"}},
		// already let through: kept as it is
		{[]string{"PATH=/bin", "IS_SANDBOX=1"}, []string{"PATH=/bin", "IS_SANDBOX=1"}},
		{[]string{"PATH=/bin", "CLAUDE_CODE_BUBBLEWRAP=1"}, []string{"PATH=/bin", "CLAUDE_CODE_BUBBLEWRAP=1"}},
	} {
		if got := local.asRoot(slices.Clone(c.env)); !slices.Equal(got, c.want) {
			t.Errorf("root %q: %q, want %q", c.env, got, c.want)
		}
	}

	// a WSL distro's user decides, not uni-switch's on Windows; and IS_SANDBOX
	// goes into the distro only for root
	claudeEuid = func() int { return 1000 }
	wslenv := func(env []string) string {
		for _, e := range env {
			if v, ok := strings.CutPrefix(e, "WSLENV="); ok {
				return v
			}
		}
		return ""
	}
	root := claudeCLI{wsl: &wslrun.Tool{Path: "/usr/bin/claude", Root: true}}
	env := root.env(root.asRoot(slices.Clone(base)), "")
	if !slices.Contains(env, "IS_SANDBOX=1") || !slices.Contains(strings.Split(wslenv(env), ":"), "IS_SANDBOX") {
		t.Fatalf("WSL as root: %q", env)
	}
	user := claudeCLI{wsl: &wslrun.Tool{Path: "/usr/bin/claude"}}
	env = user.env(user.asRoot(append(slices.Clone(base), "IS_SANDBOX=x")), "")
	if slices.Contains(env, "IS_SANDBOX=1") || slices.Contains(strings.Split(wslenv(env), ":"), "IS_SANDBOX") {
		t.Fatalf("WSL as a user: %q", env)
	}
}
