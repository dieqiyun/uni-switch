package gateway

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

const gwSetupToken = "sk-ant-oat01-gatewaysetuptoken-0123456789abcdefghijklmnop"

// A Claude account added by a setup-token (sfss5362 on X) is served by the
// real Claude Code, given the token in CLAUDE_CODE_OAUTH_TOKEN for runs in
// that account's own config directory and no other: never on the command
// line, never to Claude Code's own account or another saved one, and never
// the token the shell happens to hold.
func TestClaudeSetupTokenReachesItsOwnRunsOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for Claude Code")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	provider.ForgetAccounts()
	t.Cleanup(provider.ForgetAccounts)

	user, err := provider.AddClaudeSetupToken(gwSetupToken)
	if err != nil {
		t.Fatal(err)
	}
	provider.ForgetAccounts()
	// the directory the gateway is handed for the account, as a turn is
	var dir string
	for _, p := range provider.All() {
		if p.Account != nil && p.Account.Agent == "claude" && p.Account.User == user {
			d, _, err := p.Account.Token(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			dir = d
		}
	}
	if dir == "" {
		t.Fatalf("no provider serves %q", user)
	}
	if _, err := os.Stat(filepath.Join(dir, ".credentials.json")); !os.IsNotExist(err) {
		t.Fatalf("a sign-in sits in the token account's directory: %v", err)
	}

	bin := t.TempDir()
	log := filepath.Join(bin, "log")
	text := "Current session: 13% used · resets Oct 1 at 3:30pm (UTC)\n"
	b, _ := json.Marshal(map[string]any{"type": "result", "is_error": false, "num_turns": 0, "result": text})
	os.WriteFile(filepath.Join(bin, "out"), b, 0o644)
	script := "#!/bin/sh\n" +
		"{ printf 'args:'; for a in \"$@\"; do printf '[%s]' \"$a\"; done; echo; echo \"dir:$CLAUDE_CONFIG_DIR\"; echo \"token:$CLAUDE_CODE_OAUTH_TOKEN\"; } >> " + log + "\n" +
		"cat " + filepath.Join(bin, "out") + "\n"
	testenv.Program(t, filepath.Join(bin, "claude"), script)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "leaked")

	run := func(configDir string) string {
		t.Helper()
		os.Remove(log)
		if _, err := claudeUsage(context.Background(), configDir); err != nil {
			t.Fatalf("%q: %v", configDir, err)
		}
		got, _ := os.ReadFile(log)
		return string(got)
	}
	if got := run(dir); !strings.Contains(got, "token:"+gwSetupToken+"\n") || !strings.Contains(got, "dir:"+dir+"\n") {
		t.Fatalf("the token account's run wasn't given its token:\n%s", got)
	} else if args, _, _ := strings.Cut(got, "\n"); strings.Contains(args, gwSetupToken) {
		t.Fatalf("the token went on the command line: %s", args)
	}
	for _, other := range []string{"", filepath.Join(filepath.Dir(dir), "0123456789abcdef")} {
		if got := run(other); !strings.Contains(got, "token:\n") {
			t.Fatalf("a run in %q was given a token:\n%s", other, got)
		}
	}

	// the bridge's env is built the same way
	env := inClaudeDir([]string{"CLAUDE_CODE_OAUTH_TOKEN=leaked", "PATH=/bin"}, dir)
	n := 0
	for _, e := range env {
		if strings.HasPrefix(e, "CLAUDE_CODE_OAUTH_TOKEN=") {
			n++
			if e != "CLAUDE_CODE_OAUTH_TOKEN="+gwSetupToken {
				t.Fatalf("bridge env %q", e)
			}
		}
	}
	if n != 1 {
		t.Fatalf("%d tokens in the bridge's env: %q", n, env)
	}
}
