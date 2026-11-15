package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// #4747: a server given to Claude Code is written into its .claude.json
// under the lock Claude Code saves the file under (<the file's real
// path>.lock, a directory): held while the file is read and written back,
// or a save of its own between the two — project state, tips, usage
// counters — is lost.

// A server is not written while Claude Code holds the lock, and goes in
// once it lets it go, the lock taken and let go as Claude Code takes and
// lets its own go.
func TestClaudeJSONMCPWaitsForClaudeCodesLock(t *testing.T) {
	h := sandbox(t)
	profile := filepath.Join(h, ".claude.json")
	write(t, profile, "{}\n")
	lock := profile + ".lock"
	if err := os.Mkdir(lock, 0o755); err != nil { // Claude Code holds it
		t.Fatal(err)
	}
	done := make(chan *Result, 1)
	errs := make(chan error, 1)
	go func() {
		r, err := SaveServer("", Server{Name: "fs", Transport: "stdio", Command: "npx", Args: []string{"-y", "@mcp/fs"}, Agents: []string{"claude"}})
		done <- r
		errs <- err
	}()
	// it waits as long as the lock is held, and touched
	for range 5 {
		select {
		case <-done:
			t.Fatal("saved while Claude Code held its lock")
		case <-time.After(100 * time.Millisecond):
		}
		now := time.Now()
		_ = os.Chtimes(lock, now, now)
	}
	if err := os.Remove(lock); err != nil { // Claude Code lets it go
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		if len(r.Problems) > 0 {
			t.Fatalf("problems: %+v", r.Problems)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the server never went in after the lock was let go")
	}
	if got := read(t, profile); !strings.Contains(got, "@mcp/fs") {
		t.Fatalf("the server isn't in .claude.json:\n%s", got)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("the lock left behind: %v", err)
	}
}

// A lock its holder left (not touched for longer than Claude Code's stale
// time for the file, 10s) is taken over, as Claude Code does.
func TestClaudeJSONMCPTakesAStaleLock(t *testing.T) {
	h := sandbox(t)
	profile := filepath.Join(h, ".claude.json")
	write(t, profile, "{}\n")
	lock := profile + ".lock"
	if err := os.Mkdir(lock, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Minute)
	_ = os.Chtimes(lock, old, old)
	r, err := SaveServer("", Server{Name: "fs", Transport: "stdio", Command: "npx", Args: []string{"-y", "@mcp/fs"}, Agents: []string{"claude"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Problems) > 0 {
		t.Fatalf("problems: %+v", r.Problems)
	}
	if got := read(t, profile); !strings.Contains(got, "@mcp/fs") {
		t.Fatalf("the server isn't in .claude.json:\n%s", got)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("the lock left behind: %v", err)
	}
}
