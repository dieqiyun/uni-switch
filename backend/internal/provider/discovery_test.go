//go:build !windows

package provider

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// openWatch turns each path into a FIFO and records every open of one for
// reading. A FIFO's reader waits in open until a writer comes, and a
// non-blocking open for writing succeeds only while a reader is there, so
// a loop of such opens sees each read the moment it starts: whoever opened
// it is handed "{}" and an end of file, and the path is noted. A plain
// file would be read with nothing to show for it.
//
// The func returned waits for a whole loop that began after the call, so
// an open the loop found before then is in what it returns.
func openWatch(t *testing.T, paths []string) func() []string {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(p, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var (
		mu     sync.Mutex
		opened []string
		loops  int
	)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			for _, p := range paths {
				fd, err := syscall.Open(p, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
				if err != nil {
					continue // ENXIO: nobody is reading it
				}
				mu.Lock()
				opened = append(opened, p)
				mu.Unlock()
				_, _ = syscall.Write(fd, []byte("{}"))
				_ = syscall.Close(fd)
			}
			mu.Lock()
			loops++
			mu.Unlock()
			time.Sleep(200 * time.Microsecond)
		}
	}()
	// a FIFO left behind blocks the next reader for good: Kiro's IDE folder
	// is the package's, not this test's (kiro_main_test.go)
	t.Cleanup(func() {
		close(stop)
		<-done
		for _, p := range paths {
			os.Remove(p)
		}
	})
	return func() []string {
		mu.Lock()
		// the loop under way may have opened a FIFO and not noted it yet;
		// the one after it begins after this call
		for until := loops + 2; loops < until; {
			mu.Unlock()
			time.Sleep(100 * time.Microsecond)
			mu.Lock()
		}
		defer mu.Unlock()
		return slices.Clone(opened)
	}
}

// recordingTools puts a program first on PATH for each name that notes
// that it ran, and fails; the notes are read back by the func returned.
func recordingTools(t *testing.T, names []string) func() string {
	t.Helper()
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "ran.log")
	for _, n := range names {
		script := "#!/bin/sh\necho \"" + n + " $*\" >> '" + log + "'\nexit 1\n"
		if err := os.WriteFile(filepath.Join(bin, n), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() string {
		b, _ := os.ReadFile(log)
		return string(b)
	}
}

// With subscription account discovery off (#1535, MaxHaiCom), no route
// into the accounts — listing them, their usage, renewing, warming,
// switching, session identities, check-ins, moves onto plugins — opens an
// agent's credential file or runs the Keychain's `security`, Linux's
// secret-tool or an agent's CLI to ask who is signed in.
func TestAccountDiscoveryOffReadsNoSignIn(t *testing.T) {
	limitsHome(t)
	home, _ := os.UserHomeDir()
	cfg := os.Getenv("XDG_CONFIG_HOME")
	oldClaude := claudeKeychain
	claudeKeychain = true // every OS: the Keychain is asked, by the fake
	t.Cleanup(func() { claudeKeychain = oldClaude; ForgetAccounts() })
	ForgetAccounts()

	if err := settings.Save(settings.Settings{NoAccountDiscovery: true}); err != nil {
		t.Fatal(err)
	}
	ran := recordingTools(t, []string{"security", "secret-tool", "claude", "codex", "cursor-agent", "devin", "grok", "kiro-cli", "copilot"})
	files := []string{
		claudeCredentialsPath(),
		filepath.Join(home, ".claude.json"),
		codexAuthPath(),
		filepath.Join(copilotCLIHome(), "config.json"),
		filepath.Join(cfg, "github-copilot", "apps.json"),
		filepath.Join(cfg, "github-copilot", "hosts.json"),
		cursorAuthFile(),
		DevinCredentialsPath(),
		filepath.Join(geminiDir(), "oauth_creds.json"),
		zcodeCredentialsPath(),
		cmdAuthPath(),
		filepath.Join(kiroIDEDir(), "kiro-auth-token.json"),
		filepath.Join(home, ".local", "share", "opencode", "auth.json"),
	}
	slices.Sort(files)
	files = slices.Compact(files)
	opened := openWatch(t, files)

	// the watch sees a read: this test's own. A reader slow to reach the
	// end is handed "{}" again by the next loop's write, and noted again,
	// so under load it reads "{}{}"
	if b, err := os.ReadFile(codexAuthPath()); err != nil || len(b) == 0 || strings.ReplaceAll(string(b), "{}", "") != "" {
		t.Fatalf("the watch didn't answer a read: %q %v", b, err)
	}
	if got := opened(); len(got) == 0 || slices.ContainsFunc(got, func(p string) bool { return p != codexAuthPath() }) {
		t.Fatalf("the watch saw %v, want the test's own read", got)
	}
	checked := len(opened())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if ps := Accounts(); len(ps) != 0 {
		t.Errorf("Accounts with discovery off: %d", len(ps))
	}
	for _, p := range All() {
		if p.Account != nil {
			t.Errorf("All serves the account %s", p.ID)
		}
	}
	if ls := Logins(""); len(ls) != 0 {
		t.Errorf("Logins: %v", ls)
	}
	for _, agent := range []string{"claude", "codex", "copilot", "cursor", "devin", "kiro"} {
		Logins(agent)
		LoginUsage(ctx, agent)
		Allowances(agent)
		InUseLogin(agent)
		NextLogin(ctx, agent)
		if err := SwitchLogin(agent, "someone@example.com"); err == nil {
			t.Errorf("SwitchLogin %s with discovery off", agent)
		}
		if err := ForgetLogin(agent, "someone@example.com"); err == nil {
			t.Errorf("ForgetLogin %s with discovery off", agent)
		}
		if err := SetLoginOn(agent, "someone@example.com", false); err == nil {
			t.Errorf("SetLoginOn %s with discovery off", agent)
		}
		if _, err := StartSignIn(agent); err == nil {
			t.Errorf("StartSignIn %s with discovery off", agent)
		}
	}
	SubscriptionUsage(ctx)
	RenewLogins(ctx, 0)
	SessionIdentities(filepath.Dir(codexAuthPath()))
	CodexAPIKeySignedIn()
	CodexUsedUp(ctx)
	ClaudeLoginVersion("someone@example.com")
	claudeIdentity()
	cursorIdentity()
	devinIdentity()
	cursorTokenOf(true)
	readKiroCLI()
	readKiroIDE()
	copilotCLILogin()
	_, _, _ = DevinAuth()
	MoveCandidates()
	MoveRetiring(ctx)
	HandOver(ctx, true)
	CheckInWorkBuddy(ctx)
	CheckInMiniMax(ctx)
	CheckInTrae(ctx)
	CheckInQoder(ctx)
	CheckInPlugins(ctx)
	if _, err := ImportLogins(ctx, "codex", []string{`{"tokens":{"access_token":"a"}}`}); err == nil {
		t.Error("ImportLogins with discovery off")
	}
	if _, err := ImportFactoryKeys(ctx, []string{"fk-0123456789abcdef0123456789abcdef"}); err == nil {
		t.Error("ImportFactoryKeys with discovery off")
	}

	// anything started in the background has its moment
	time.Sleep(300 * time.Millisecond)
	if got := opened()[checked:]; len(got) != 0 {
		read := slices.Compact(slices.Sorted(slices.Values(got)))
		t.Errorf("with discovery off, %d reads of sign-ins: %s", len(got), strings.Join(read, " "))
	}
	if s := ran(); s != "" {
		t.Errorf("with discovery off, these ran:\n%s", s)
	}
}

// UNI_SWITCH_NO_ACCOUNTS turns discovery off for the process, and never on
// what the user saved off.
func TestAccountDiscoverySetting(t *testing.T) {
	limitsHome(t)
	t.Setenv("UNI_SWITCH_NO_ACCOUNTS", "")
	if settings.AccountDiscoveryOff() {
		t.Fatal("off by default")
	}
	for v, off := range map[string]bool{"1": true, "true": true, "yes": true, "0": false, "false": false, "FALSE": false} {
		t.Setenv("UNI_SWITCH_NO_ACCOUNTS", v)
		if got := settings.AccountDiscoveryOff(); got != off {
			t.Errorf("UNI_SWITCH_NO_ACCOUNTS=%s: off %v, want %v", v, got, off)
		}
	}
	if err := settings.Save(settings.Settings{NoAccountDiscovery: true}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UNI_SWITCH_NO_ACCOUNTS", "0")
	if !settings.AccountDiscoveryOff() {
		t.Error("the environment turned on what the user saved off")
	}
	if err := SwitchLogin("codex", "x@example.com"); err == nil || !strings.Contains(err.Error(), "uni-switch accounts discovery on") {
		t.Errorf("the refusal says how to turn it back on: %v", err)
	}
}
