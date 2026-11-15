package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/claudecode"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/libc"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// claudeCodeOut runs `uni-switch claude-code args…` and gives what it printed.
func claudeCodeOut(t *testing.T, args ...string) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	var out strings.Builder
	done := make(chan struct{})
	go func() {
		b, _ := io.ReadAll(r)
		out.Write(b)
		close(done)
	}()
	err = claudeCodeCmd(args)
	w.Close()
	os.Stdout = old
	<-done
	r.Close()
	return out.String(), err
}

// `uni-switch claude-code install --yes` on a machine with no Claude Code (a
// container, Jorben on Discord) downloads the release's build, says from
// where and to where, and the subscription then runs it.
func TestClaudeCodeInstallDownloadsTheRelease(t *testing.T) {
	platform, err := claudecode.Platform(runtime.GOOS, runtime.GOARCH, false)
	if err != nil {
		t.Skip(err)
	}
	if libc.Musl() {
		t.Skip("a musl Linux asks for the -musl build")
	}
	const body = "#!/bin/sh\necho '2.1.296 (Claude Code)'\n"
	sum := sha256.Sum256([]byte(body))
	exe := map[bool]string{true: "claude.exe", false: "claude"}[runtime.GOOS == "windows"]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprint(w, "2.1.296")
		case "/2.1.296/manifest.json":
			fmt.Fprintf(w, `{"version":"2.1.296","manifestSignatureEnforcement":"flag","platforms":{%q:{"binary":%q,"checksum":%q,"size":%d}}}`,
				platform, exe, hex.EncodeToString(sum[:]), len(body))
		case "/2.1.296/" + platform + "/" + exe:
			fmt.Fprint(w, body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	oldBase, oldOwn := claudecode.Base, claudeCode.own
	claudecode.Base, claudeCode.own = srv.URL, func() string { return "" }
	t.Cleanup(func() { claudecode.Base, claudeCode.own = oldBase, oldOwn; _ = claudecode.Remove() })

	out, err := claudeCodeOut(t)
	if err != nil || !strings.Contains(out, "uni-switch claude-code install") {
		t.Fatalf("status before: %q, %v", out, err)
	}
	out, err = claudeCodeOut(t, "install", "--yes")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	for _, want := range []string{"2.1.296", srv.URL + "/2.1.296/" + platform + "/" + exe, claudecode.Root(), "SHA-256", "downloaded and checked"} {
		if !strings.Contains(out, want) {
			t.Errorf("install didn't say %q:\n%s", want, out)
		}
	}
	if p, v := claudecode.Downloaded(); v != "2.1.296" || p == "" {
		t.Fatalf("Downloaded() = %q, %q", p, v)
	}
	if out, _ := claudeCodeOut(t, "install", "--yes"); !strings.Contains(out, "downloaded already") {
		t.Errorf("a second install: %q", out)
	}
	if out, _ := claudeCodeOut(t); !strings.Contains(out, "downloaded by uni-switch") {
		t.Errorf("status after: %q", out)
	}
	if _, err := claudeCodeOut(t, "remove"); err != nil {
		t.Fatal(err)
	}
	if p, _ := claudecode.Downloaded(); p != "" {
		t.Errorf("remove left %s", p)
	}
}

// `uni-switch claude-code runs N` is the user's own bound on Claude Code
// processes (#2956), which the gateway's 503 names; auto gives it back to
// the machine's memory, and a value that isn't a count is refused and
// writes nothing.
func TestClaudeCodeRunsSetsTheUsersOwn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	noGateway(t)
	out, err := claudeCodeOut(t, "runs", "3")
	if err != nil {
		t.Fatal(err)
	}
	if got := settings.Load().ClaudeRunsMost; got != 3 {
		t.Fatalf("claudeRunsMost is %d after `runs 3`", got)
	}
	if !strings.Contains(out, "3 Claude Code processes may run at once") || !strings.Contains(out, "your setting") {
		t.Fatalf("runs 3 said:\n%s", out)
	}
	for _, bad := range []string{"0", "-2", "lots", "--yes"} {
		if _, err := claudeCodeOut(t, "runs", bad); err == nil {
			t.Fatalf("runs %s was taken", bad)
		}
		if got := settings.Load().ClaudeRunsMost; got != 3 {
			t.Fatalf("runs %s wrote %d", bad, got)
		}
	}
	if _, err := claudeCodeOut(t, "runs", "--help"); err != nil {
		t.Fatal(err)
	}
	if got := settings.Load().ClaudeRunsMost; got != 3 {
		t.Fatalf("runs --help wrote %d", got)
	}
	if _, err := claudeCodeOut(t, "runs", "auto"); err != nil {
		t.Fatal(err)
	}
	if got := settings.Load().ClaudeRunsMost; got != 0 {
		t.Fatalf("runs auto left %d", got)
	}
	out, err = claudeCodeOut(t, "runs")
	if err != nil || strings.Contains(out, "your setting") {
		t.Fatalf("runs after auto said (%v):\n%s", err, out)
	}
}

// noGateway has `uni-switch claude-code runs` find no uni-switch running, never
// the one on this computer.
func noGateway(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()
	old := claudeCode.gateway
	claudeCode.gateway = func() string { return base }
	t.Cleanup(func() { claudeCode.gateway = old })
}

// `uni-switch claude-code runs` lists the Claude Code processes the running
// uni-switch has for Claude subscriptions, each with its pid, model, what it
// waits for, until when and its account, and says what they are: a task
// manager names them by Claude Code's version and "uni-switch", with nothing
// to tell why so many run (@Candydidi3 on X). With no uni-switch answering,
// it says none of its Claude Code runs.
func TestClaudeCodeRunsListsTheProcesses(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	now := time.Now()
	runs := gateway.ClaudeRuns{Object: "claude-runs", Live: 3, IdleMost: 6, IdleFor: time.Hour, Runs: []gateway.ClaudeRun{
		{PID: 4512, Model: "claude-opus-4-7", Account: "me@example.com", State: "answering", Since: now.Add(-12 * time.Second), Started: now.Add(-time.Minute)},
		{PID: 4520, Model: "claude-opus-4-7", Account: "me@example.com", State: "idle", Since: now.Add(-4 * time.Minute), Ends: now.Add(56*time.Minute + 30*time.Second), RSS: 180 << 20},
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != gateway.ClaudeRunsPath {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(runs)
	}))
	defer srv.Close()
	old := claudeCode.gateway
	claudeCode.gateway = func() string { return srv.URL }
	t.Cleanup(func() { claudeCode.gateway = old })
	out, err := claudeCodeOut(t, "runs")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"2 Claude Code running for Claude subscription conversations, 1 more for a test, warm-up or usage read",
		"pid 4512", "answering 12s",
		"pid 4520", "kept for its next turn 4m", "ends in 56m", "180.0 MB", "me@example.com",
		"claude-mcp-helper", "by its version",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("runs says no %q:\n%s", want, out)
		}
	}
	noGateway(t)
	out, err = claudeCodeOut(t, "runs")
	if err != nil || !strings.Contains(out, "none of its Claude Code is running") {
		t.Fatalf("with no uni-switch running, runs said (%v):\n%s", err, out)
	}
}
