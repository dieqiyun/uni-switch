package proc

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The Codex app's app-server is restarted only when it runs on uni-switch's
// Codex home: its HOME and CODEX_HOME are read from what ps -E prints
// (macOS) or /proc/<pid>/environ (Linux).
func TestParsePSEnv(t *testing.T) {
	line := " /Applications/ChatGPT.app/Contents/Resources/codex app-server --analytics-default-enabled TMPDIR=/var/folders/x/T/ HOME=/Users/me/sand box CODEX_HOME=/Users/me/sand box/.codex LANG=en_US.UTF-8"
	got := parsePSEnv(line, []string{"HOME", "CODEX_HOME", "PATH"})
	want := map[string]string{"HOME": "/Users/me/sand box", "CODEX_HOME": "/Users/me/sand box/.codex"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsePSEnv = %v, want %v", got, want)
	}
	// a name in the command line is not the environment's: the last is
	got = parsePSEnv(" codex app-server -c HOME=/nope HOME=/Users/me", []string{"HOME"})
	if got["HOME"] != "/Users/me" {
		t.Fatalf("HOME = %q", got["HOME"])
	}
	// the last variable runs to the end of the line
	got = parsePSEnv(" codex app-server HOME=/Users/me\n", []string{"HOME"})
	if got["HOME"] != "/Users/me" {
		t.Fatalf("last HOME = %q", got["HOME"])
	}
}

func TestParseEnviron(t *testing.T) {
	b := []byte("PATH=/usr/bin\x00HOME=/home/me\x00CODEX_HOME=/home/me/x=y\x00junk\x00")
	got := parseEnviron(b, []string{"HOME", "CODEX_HOME", "LANG"})
	want := map[string]string{"HOME": "/home/me", "CODEX_HOME": "/home/me/x=y"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseEnviron = %v, want %v", got, want)
	}
}

// TestEnvOfHelper is TestEnvOfChild's child: it says it is running and
// waits to be ended. macOS keeps a system binary's environment (sleep's)
// from ps, so the child is this test binary, as the Codex app's codex is
// its own.
func TestEnvOfHelper(t *testing.T) {
	if os.Getenv("UNI_SWITCH_ENV_HELPER") != "1" {
		return
	}
	os.Stdout.WriteString("running\n")
	time.Sleep(30 * time.Second)
}

// EnvOf reads a running child's own environment, not uni-switch's.
func TestEnvOfChild(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("EnvOf answers ErrNoEnv here")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := CommandContext(ctx, os.Args[0], "-test.run=^TestEnvOfHelper$")
	// TMPDIR is this test's: given no testenv.Marker, the helper makes a
	// test home of its own there, which it never removes once killed
	cmd.Env = []string{"UNI_SWITCH_ENV_HELPER=1", "HOME=/sand box/h", "CODEX_HOME=/sand box/h/.codex", "TMPDIR=" + t.TempDir()}
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })
	// running, so its exec is done: Start returns before Linux has written
	// the new program's environment, and /proc/<pid>/environ read then is
	// empty (#3906)
	if line, _ := bufio.NewReader(out).ReadString('\n'); line != "running\n" {
		t.Fatalf("the helper: %q", line)
	}
	got, err := EnvOf(context.Background(), cmd.Process.Pid, "HOME", "CODEX_HOME", "LANG")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"HOME": "/sand box/h", "CODEX_HOME": "/sand box/h/.codex"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EnvOf = %v, want %v", got, want)
	}
}

// End ends a process as a clean exit, which the Codex app takes for an
// app-server that quit, not one that crashed: code 0 on Windows (any other
// is shown as a fatal error), SIGTERM elsewhere.
func TestEndExitsClean(t *testing.T) {
	cmd := CommandContext(context.Background(), os.Args[0], "-test.run=^TestEnvOfHelper$")
	cmd.Env = append(os.Environ(), "UNI_SWITCH_ENV_HELPER=1")
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	// running, so the signal isn't taken before its handlers
	if line, _ := bufio.NewReader(out).ReadString('\n'); line != "running\n" {
		t.Fatalf("the helper: %q", line)
	}
	if err := End(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("still running 20s after End")
	}
	if runtime.GOOS == "windows" {
		if err != nil {
			t.Fatalf("exit after End: %v, want code 0", err)
		}
		return
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) || !strings.Contains(ee.String(), "terminated") {
		t.Fatalf("exit after End: %v, want terminated by SIGTERM", err)
	}
}
