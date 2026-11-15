package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/claudecode"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// A provider switched off, or whose account was removed from uni-switch, gets
// no request from uni-switch in the background: its CLI isn't asked who is
// signed in, its allowance isn't read, its sign-in isn't renewed or
// switched, its list isn't fetched (Riko on Discord: with Claude switched
// off, Surge showed `claude auth status --json` every minute). Fake
// claude and devin CLIs on PATH log every run, and what the
// gateway's loops and the window's pages do is run with each of them
// stale, as minutes passing leave them.
func TestLeftAloneRunsNoCLI(t *testing.T) {
	shellFakes(t)
	findDevin := DevinExecutable
	home := claudeHome(t)
	claudeSignIn(t, home, time.Now().Add(24*time.Hour))
	writeFile(t, filepath.Join(home, ".claude.json"), map[string]any{"oauthAccount": map[string]any{"emailAddress": "me@example.com"}})

	bin, log := t.TempDir(), filepath.Join(t.TempDir(), "runs")
	for _, name := range []string{"claude", "devin"} {
		testenv.Program(t, filepath.Join(bin, name), "#!/bin/sh\necho \""+name+" $*\" >> "+log+"\n"+
			"echo '{\"loggedIn\":true,\"email\":\"me@example.com\",\"subscriptionType\":\"max\"}'\n")
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
	// the real lookups, which find them on PATH
	claudeExecutable, DevinExecutable = claudecode.Find, findDevin
	forgetClaudeStatus()
	t.Cleanup(forgetClaudeStatus)

	runs := func() []string {
		b, _ := os.ReadFile(log)
		return strings.Fields(strings.ReplaceAll(strings.TrimSpace(string(b)), " ", "_"))
	}
	stale := func() {
		claudeStatusMu.Lock()
		if !claudeStatusAt.IsZero() {
			claudeStatusAt = time.Now().Add(-time.Hour)
		}
		claudeStatusMu.Unlock()
		for _, c := range []*cliIdentity{cursorStatus, devinStatus} {
			c.Lock()
			if !c.at.IsZero() {
				c.at = time.Now().Add(-time.Hour)
			}
			c.Unlock()
		}
		loginsMu.Lock()
		loginsSeenAt.Store(0)
		loginsMu.Unlock()
	}
	loop := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for range 3 {
			stale()
			All()
			Hidden()
			fetchSubscriptionUsage(ctx)
			for _, agent := range []string{"claude", "devin"} {
				LoginUsage(ctx, agent)
			}
			RenewLogins(ctx, 0)
			SwitchWhenSpent(ctx, "claude")
			FetchNew(time.Second)
		}
	}

	save := func(off, hidden bool) {
		writeFile(t, Path(), map[string]any{"providers": []map[string]any{
			{"id": "claude", "off": off}, {"id": "devin", "hidden": hidden},
		}})
	}
	save(true, true)
	loop()
	if got := runs(); len(got) != 0 {
		t.Fatalf("switched off or removed, the CLIs were run: %v", got)
	}
	// the account is still there to switch on again, its CLI never asked
	if p, ok := find(All(), "claude"); !ok || !p.Off {
		t.Fatalf("claude switched off: %+v %v", p, ok)
	}

	// switched on, the same loop asks them: the fakes are the ones found.
	// Devin's built-in is retired, so nothing here asks its CLI either way.
	save(false, false)
	loop()
	got := strings.Join(runs(), " ")
	for _, want := range []string{"claude_auth_status_--json"} {
		if !strings.Contains(got, want) {
			t.Errorf("switched on, no %s among %s", want, got)
		}
	}
	// what was asked behind the look has answered before the test ends
	for _, c := range []*cliIdentity{cursorStatus, devinStatus} {
		c.Lock()
		asking, done := c.refreshing, c.done
		c.Unlock()
		if asking {
			<-done
		}
	}
	for {
		claudeStatusMu.Lock()
		busy := claudeStatusBusy
		claudeStatusMu.Unlock()
		if !busy {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}
