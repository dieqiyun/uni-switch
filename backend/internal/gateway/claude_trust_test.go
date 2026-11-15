package gateway

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/claudetui"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
	"github.com/dieqiyun/uni-switch/backend/internal/wslrun"
)

// savedClaudeDir is a saved Claude account's config directory, as
// uni-switch keeps one (provider's claudeAccountDir), in a home of the
// test's own.
func savedClaudeDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	testenv.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	dir := filepath.Join(filepath.Dir(provider.Path()), "claude-accounts", "0123456789abcdef")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// a saved account as uni-switch writes it: onboarded, no projects
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(`{"hasCompletedOnboarding": true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// trustedIn is the projects .claude.json b records as trusted.
func trustedIn(t *testing.T, b []byte) []string {
	t.Helper()
	var m struct {
		Projects map[string]struct {
			Trusted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	var out []string
	for k, p := range m.Projects {
		if p.Trusted {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// claudeKey is the key Claude Code 2.1.316 looks folder work up by: the
// path its links lead to, with forward slashes on Windows.
func claudeKey(work string) string { return filepath.ToSlash(evalSymlinks(work)) }

// mydosu's saved account (#4830, Windows, Claude Code 2.1.316) was asked
// whether to trust uni-switch's folder on every /usage read and every test,
// and each waited out its caller's time on the question (a /usage read
// runs as -p now, #5582, which asks nothing about trust). Before a test
// runs, the account's .claude.json says the folder is trusted, under the
// path Claude Code runs in, which a Mac's getcwd gives with its links
// resolved (/private/var/… for /var/…).
func TestClaudeOneOffTrustsUniSwitchsFolder(t *testing.T) {
	for _, c := range []struct {
		name string
		run  func(ctx context.Context, cfg string) error
		say  string
	}{
		{"test", func(ctx context.Context, cfg string) error { return askClaude(ctx, cfg, "haiku") }, `"result":"Hi!"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			fakeWarmClaude(t, `{}`, 0)
			claudeTUIRuns = true
			t.Cleanup(func() { claudeTUIRuns = false })
			t.Setenv("TMPDIR", t.TempDir())
			cfg := savedClaudeDir(t)
			seen := filepath.Join(t.TempDir(), "seen")
			helper := filepath.Join(t.TempDir(), "uni-switch")
			// what the account's file says as Claude Code starts
			testenv.Program(t, helper, `#!/bin/sh
[ "$1" = "`+claudetui.Command+`" ] || exit 9
cat "$CLAUDE_CONFIG_DIR/.claude.json" > '`+seen+`'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,`+c.say+`}'
cat > /dev/null
`)
			claudeTUIHelper = helper
			t.Cleanup(func() { claudeTUIHelper = "" })
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := c.run(ctx, cfg); err != nil {
				t.Fatal(err)
			}
			work, err := claudeWorkDir()
			if err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(seen)
			if err != nil {
				t.Fatal(err)
			}
			got := trustedIn(t, b)
			if !slices.Contains(got, claudeKey(work)) {
				t.Fatalf("Claude Code started with %v trusted, not uni-switch's folder %s: %s", got, claudeKey(work), b)
			}
			if !strings.Contains(string(b), `"hasCompletedOnboarding": true`) {
				t.Fatalf("the account's other keys went: %s", b)
			}
		})
	}
}

// The account Claude Code is signed in to (configDir "") keeps its own
// ~/.claude.json, which uni-switch never writes, and a folder of the run's
// own, made when uni-switch's can't be used, isn't recorded: each would
// leave one more trusted project behind.
func TestClaudeTrustWorkOnlyUniSwitchsFolderInSavedAccounts(t *testing.T) {
	tmp := t.TempDir()
	for _, v := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(v, tmp)
	}
	cfg := savedClaudeDir(t)
	home, _ := os.UserHomeDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	work, err := claudeWorkDir()
	if err != nil {
		t.Fatal(err)
	}
	trustWork(claudeCLI{}, "", work)
	for _, f := range []string{filepath.Join(home, ".claude.json"), filepath.Join(home, ".claude", ".claude.json")} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("%s was written", f)
		}
	}
	own := t.TempDir()
	trustWork(claudeCLI{}, cfg, own)
	b, _ := os.ReadFile(filepath.Join(cfg, ".claude.json"))
	if got := trustedIn(t, b); len(got) != 0 {
		t.Errorf("a folder of the run's own was recorded: %v", got)
	}
	trustWork(claudeCLI{}, cfg, work)
	b, _ = os.ReadFile(filepath.Join(cfg, ".claude.json"))
	if got := trustedIn(t, b); !slices.Contains(got, claudeKey(work)) {
		t.Errorf("uni-switch's folder wasn't recorded: %v", got)
	}
}

// Claude Code 2.1.316 keys a project on Windows by its path with forward
// slashes (C:/Users/<me>/AppData/Local/Temp/uni-switch-claude-work): an entry
// under the backslashed path is never found, and the question is asked.
func TestClaudeTrustKeysAsClaudeCodeKeys(t *testing.T) {
	old := trustGOOS
	t.Cleanup(func() { trustGOOS = old })
	trustGOOS = "windows"
	// mydosu's folder, a name in NFD as a file system may give it
	got := claudeTrustKeys(claudeCLI{}, "C:\\Users\\Me\u0301\\AppData\\Local\\Temp\\uni-switch-claude-work")
	if want := "C:/Users/M\u00e9/AppData/Local/Temp/uni-switch-claude-work"; !slices.Contains(got, want) {
		t.Errorf("keys %q, want %q among them", got, want)
	}
	for _, k := range got {
		if strings.Contains(k, `\`) {
			t.Errorf("key %q has a backslash", k)
		}
	}
	trustGOOS = "linux"
	if runtime.GOOS != "windows" {
		if got := claudeTrustKeys(claudeCLI{}, `/tmp/a\b`); !slices.Contains(got, `/tmp/a\b`) {
			t.Errorf("a backslash in a Unix name was changed: %v", got)
		}
	}
	got = claudeTrustKeys(claudeCLI{wsl: &wslrun.Tool{Mount: "/mnt/"}}, `C:\Users\me\AppData\Local\Temp\uni-switch-claude-work`)
	if len(got) != 1 || got[0] != "/mnt/c/Users/me/AppData/Local/Temp/uni-switch-claude-work" {
		t.Errorf("Claude Code in WSL runs in its mount of the folder, keys %v", got)
	}
}

// A saved account's bridge run in uni-switch's folder starts with the folder
// trusted too: the bridge's runs work there whenever the caller's own
// folder isn't one here.
func TestClaudeBridgeTrustsUniSwitchsFolderForSavedAccount(t *testing.T) {
	h := tuiScriptHarness(t, `cat "$CLAUDE_CONFIG_DIR/.claude.json" > $log.seen`)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(os.Getenv("HOME"), ".config"))
	cfg := filepath.Join(filepath.Dir(provider.Path()), "claude-accounts", "0123456789abcdef")
	if err := os.MkdirAll(cfg, 0o700); err != nil {
		t.Fatal(err)
	}
	req := &Request{Messages: []Message{{Role: "user", Parts: []Part{{Kind: Text, Text: "hello"}}}}}
	run, _, err := h.s.subscription.start(context.Background(), req, "claude-sonnet-5", cfg, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !run.tui {
		t.Fatal("the run wasn't interactive")
	}
	var b []byte
	eventually(t, "the run read its account's file", func() bool {
		entries, _ := os.ReadDir(h.logs)
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".seen") {
				b, _ = os.ReadFile(filepath.Join(h.logs, e.Name()))
			}
		}
		return len(b) > 0
	})
	work, err := claudeWorkDir()
	if err != nil {
		t.Fatal(err)
	}
	if got := trustedIn(t, b); !slices.Contains(got, claudeKey(work)) {
		t.Fatalf("the run started with %v trusted, not uni-switch's folder %s", got, claudeKey(work))
	}
}
