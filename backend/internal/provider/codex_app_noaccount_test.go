package provider

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// With Codex on a provider that takes no ChatGPT account (uni-switch's table
// with requires_openai_auth = false, danzi002 on Discord: "requires_openai_auth
// = false 的时候就不应该重启 codex 了"), the app-server sends the
// provider's own key and reads no account at all, so switching the account
// in auth.json changes nothing it goes by: neither a switch nor a switch
// off a spent account restarts the Codex app's app-server, and neither the
// app nor the background daemon is said to be left on the old account. A
// route change still restarts it, and with the provider taking the account
// again a switch does too.
func TestCodexNoAccountSwitchRestartsNothing(t *testing.T) {
	signIn(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	s := stubCodexApp(t)
	old, oldHome, oldNo := listProcesses, processCodexHome, CodexNoAccount
	t.Cleanup(func() {
		listProcesses, processCodexHome, CodexNoAccount = old, oldHome, oldNo
		DismissCodexDaemon()
		DismissCodexApp()
	})
	processCodexHome = func(int) (string, bool) { return "", false }
	listProcesses = func(context.Context) ([]proc.Process, error) {
		ps := []proc.Process{{PID: 9, Args: "codex app-server --managed-daemon"}}
		for _, p := range s.running {
			ps = append(ps, proc.Process{PID: p, Args: codexWindowsAppServer})
		}
		return ps, nil
	}
	noAccount := true
	CodexNoAccount = func() bool { return noAccount }
	now := time.Now()
	idle := func() (int, time.Time) { return 0, now.Add(-time.Minute) }
	step := func() []int { return codexAppStep(context.Background(), now, idle) }
	recheck := func() {
		codexDaemonMu.Lock()
		codexDaemonChecked = time.Now()
		codexDaemonMu.Unlock()
	}

	s.route, s.running = "uni-switch,noauth", []int{60}
	if got := step(); got != nil {
		t.Fatalf("nothing changed: ended %v", got)
	}
	noteCodexSwitch("a@x", "b@x")
	recheck()
	for range 3 {
		if got := step(); got != nil {
			t.Fatalf("switched on a provider that takes no account: ended %v", got)
		}
	}
	if got := CodexAppStale(); got != "" {
		t.Fatalf("the Codex app said to be left on %q", got)
	}
	if got := CodexDaemonStale(); got != "" {
		t.Fatalf("the daemon said to be left on %q", got)
	}
	codexAppSpentSwitch()
	if got := step(); got != nil {
		t.Fatalf("switched off a spent account on a provider that takes no account: ended %v", got)
	}
	// where Codex sends its requests changed: restarted as ever
	s.route = "uni-switch,auth"
	if got := step(); !slices.Equal(got, []int{60}) {
		t.Fatalf("route changed: ended %v, want [60]", got)
	}
	// on a provider that takes the account a switch restarts it
	noAccount = false
	noteCodexSwitch("b@x", "c@x")
	recheck()
	if got := CodexAppStale(); got != "b@x" {
		t.Fatalf("on a provider that takes the account: app left on %q, want b@x", got)
	}
	if got := step(); !slices.Equal(got, []int{100}) {
		t.Fatalf("switched on a provider that takes the account: ended %v, want [100]", got)
	}
}
