package claudetui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// wslHere runs the in-distro reader's script with this machine's sh, its
// $HOME the test's.
func wslHere(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the script is the distro's sh's")
	}
	home := t.TempDir()
	old := wslReadCommand
	wslReadCommand = func(distro, sid string) *exec.Cmd {
		if distro != "Ubuntu" {
			t.Errorf("distro %q", distro)
		}
		cmd := exec.Command("/bin/sh", "-c", wslReadScript, "sh", sid)
		cmd.Env = append(os.Environ(), "HOME="+home)
		return cmd
	}
	t.Cleanup(func() { wslReadCommand = old })
	return filepath.Join(home, ".claude", "projects", "-work")
}

// waitFor waits until file path holds want.
func waitFor(t *testing.T, path, want string) {
	t.Helper()
	var got []byte
	for until := time.Now().Add(10 * time.Second); time.Now().Before(until); time.Sleep(20 * time.Millisecond) {
		if got, _ = os.ReadFile(path); string(got) == want {
			return
		}
	}
	t.Fatalf("%s holds %q, want %q", path, got, want)
}

func appendTo(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

// A transcript Windows can't read is read inside the distro: a resumed
// session's earlier turns are left out, what is written after is copied
// as it is written, and the shell ends with the run.
func TestWSLReadResumed(t *testing.T) {
	dir := wslHere(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	sid := "0f3c-1"
	file := filepath.Join(dir, sid+".jsonl")
	appendTo(t, file, `{"earlier":1}`+"\n")
	r, err := startWSLRead("Ubuntu", sid, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(filepath.Dir(filepath.Dir(r.path)), "wsl", sid+".jsonl"); r.path != want {
		t.Fatalf("copied to %s, not as a projects folder has it", r.path)
	}
	appendTo(t, file, `{"later":1}`+"\n")
	waitFor(t, r.path, `{"later":1}`+"\n")
	appendTo(t, file, `{"later":2}`+"\n")
	waitFor(t, r.path, `{"later":1}`+"\n"+`{"later":2}`+"\n")
	done := make(chan struct{})
	go func() { r.stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the shell didn't end with the run")
	}
	if r.cmd.ProcessState == nil || !r.cmd.ProcessState.Exited() {
		t.Fatalf("the shell: %v", r.cmd.ProcessState)
	}
}

// A new session's file, written after Claude Code starts, is read whole.
func TestWSLReadNewSession(t *testing.T) {
	dir := wslHere(t)
	r, err := startWSLRead("Ubuntu", "abc", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.stop)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	appendTo(t, filepath.Join(dir, "abc.jsonl"), "one\ntwo\n")
	waitFor(t, r.path, "one\ntwo\n")
}

// A session id is the bridge's, but never reaches the shell as anything
// but a word; a shell that never says it is ready fails the run.
func TestWSLReadRefuses(t *testing.T) {
	wslHere(t)
	if _, err := startWSLRead("Ubuntu", "x; rm -rf ~", t.TempDir()); err == nil {
		t.Fatal("an id with a shell's words in it")
	}
	old, oldWait := wslReadCommand, wslReadWait
	wslReadWait = 200 * time.Millisecond
	wslReadCommand = func(string, string) *exec.Cmd { return exec.Command("/bin/sh", "-c", "echo nope; cat >/dev/null") }
	t.Cleanup(func() { wslReadCommand, wslReadWait = old, oldWait })
	if _, err := startWSLRead("Ubuntu", "abc", t.TempDir()); err == nil || !strings.Contains(err.Error(), "can't be read in WSL") {
		t.Fatalf("a shell that said nope: %v", err)
	}
	wslReadCommand = func(string, string) *exec.Cmd { return exec.Command("/bin/sh", "-c", "cat >/dev/null") }
	if _, err := startWSLRead("Ubuntu", "abc", t.TempDir()); err == nil || !strings.Contains(err.Error(), "didn't start") {
		t.Fatalf("a shell that said nothing: %v", err)
	}
}
