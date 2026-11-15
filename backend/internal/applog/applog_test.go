package applog

import (
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

func TestMain(m *testing.M) { os.Exit(testenv.RunIn(m, nil)) }

// opened is open for a test, closed with the runtime's copy of it let go
// after the test: Windows removes no folder with a file open in it.
func opened(t *testing.T, path string, limit int64) *file {
	t.Helper()
	w, err := open(path, limit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
		w.Close()
	})
	return w
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// What the app logs goes to app.log in the system's logs folder, as well
// as to stderr: the app opened from the Finder had no terminal, and a
// user asked for its log had none (rayem1w on X).
func TestStartWritesTheLog(t *testing.T) {
	t.Cleanup(Stop)
	path, err := Start("0.1.test", "app")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(appdir.Logs(), Name); path != want || Current() != want {
		t.Fatalf("Start wrote %q (Current %q), want %q", path, Current(), want)
	}
	log.Println("gateway: a line the app logged")
	got := read(t, path)
	for _, want := range []string{"uni-switch 0.1.test started (app, pid", "this log: " + path, "gateway: a line the app logged"} {
		if !strings.Contains(got, want) {
			t.Errorf("the log has no %q:\n%s", want, got)
		}
	}
	if fi, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if os.PathSeparator == '/' && fi.Mode().Perm() != 0o600 {
		t.Errorf("the log is %v, want 0600: it is the user's alone", fi.Mode().Perm())
	}

	// a second Start (the app opened again in this process) goes on in the
	// same file
	if _, err := Start("0.1.test", "panel"); err != nil {
		t.Fatal(err)
	}
	log.Println("after the second start")
	if got := read(t, path); !strings.Contains(got, "gateway: a line the app logged") || !strings.Contains(got, "after the second start") {
		t.Errorf("the second start lost a line:\n%s", got)
	}

	Stop()
	log.Println("after Stop")
	if Current() != "" || strings.Contains(read(t, path), "after Stop") {
		t.Errorf("after Stop the log is still written (Current %q)", Current())
	}
}

// The log goes to stderr as well, as before; a stderr that fails (the app
// opened from Explorer has none) never keeps a line from the file.
func TestStderrToo(t *testing.T) {
	dir := t.TempDir()
	w := opened(t, filepath.Join(dir, Name), Limit)
	var errOut bytes.Buffer
	l := log.New(io.MultiWriter(quiet{&errOut}, w), "", 0)
	l.Println("both")
	l2 := log.New(io.MultiWriter(quiet{failing{}}, w), "", 0)
	l2.Println("stderr gone")
	if !strings.Contains(errOut.String(), "both") {
		t.Errorf("stderr got %q", errOut.String())
	}
	if got := read(t, filepath.Join(dir, Name)); !strings.Contains(got, "both") || !strings.Contains(got, "stderr gone") {
		t.Errorf("the file got %q", got)
	}
}

type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, os.ErrClosed }

// At its limit the log becomes app.old.log, over the one before, and a
// new one starts: two files at most, each about the limit.
func TestRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, Name)
	w := opened(t, path, 100)
	line := strings.Repeat("x", 39) + "\n" // 40 bytes
	for i := 0; i < 10; i++ {
		if n, err := w.Write([]byte(line)); err != nil || n != len(line) {
			t.Fatalf("write %d: %d, %v", i, n, err)
		}
	}
	for _, p := range []string{path, filepath.Join(dir, OldName)} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(p), err)
		}
		if fi.Size() > 100 || fi.Size() == 0 {
			t.Errorf("%s is %d bytes, want 1..100", filepath.Base(p), fi.Size())
		}
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 2 {
		t.Errorf("the folder has %d files, want 2", len(ents))
	}
}

// Another uni-switch that rotated the log already (an update's new process
// beside the old) leaves this one to open the new file, not to rotate it
// again over the old one.
func TestRotatedByAnother(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("Windows moves no file another process has open")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, Name)
	w := opened(t, path, 100)
	w.Write([]byte(strings.Repeat("a", 80) + "\n"))
	// the other process: closes nothing of ours, moves the file and starts
	// its own
	if err := os.Rename(path, filepath.Join(dir, OldName)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w.Write([]byte(strings.Repeat("b", 40) + "\n"))
	if got := read(t, filepath.Join(dir, OldName)); strings.Contains(got, "other") || strings.Contains(got, "b") {
		t.Errorf("app.old.log was rotated over: %q", got)
	}
	if got := read(t, path); !strings.HasPrefix(got, "other\n") || !strings.Contains(got, "bbbb") {
		t.Errorf("app.log %q, want the other's line, then ours", got)
	}
}

// What looks like a key or token is masked in the log, its first
// characters kept; the rest of the line is as written.
func TestScrub(t *testing.T) {
	for in, want := range map[string]string{
		"Authorization: Bearer abcdefghijklmnop1234":                                  "Authorization: Bearer …",
		"GET https://x.example/v1/models?key=AIzaSyAbc123&alt=sse":                    "GET https://x.example/v1/models?key=…&alt=sse",
		"refresh https://auth.example/t?refresh_token=rt.abcdef done":                 "refresh https://auth.example/t?refresh_token=… done",
		"upstream said: invalid key sk-ant-api03-abcdefghijklmnopqrstuv":              "upstream said: invalid key sk-ant-…",
		"key sk-proj-abcdefghijklmnopqrstuvwx refused":                                "key sk-proj-… refused",
		"google AIzaSyA1234567890abcdefghijklmnopq":                                   "google AIza…",
		"github gho_abcdefghijklmnopqrstuvwxyz0123 expired":                           "github gho_… expired",
		"jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N": "jwt eyJ…",
		"gateway: claude/claude-sonnet-4-5 200 in 1.2s (codex, 3 tries)":              "gateway: claude/claude-sonnet-4-5 200 in 1.2s (codex, 3 tries)",
		"provider: sk-short is not a key":                                             "provider: sk-short is not a key",
	} {
		if got := string(scrub([]byte(in))); got != want {
			t.Errorf("scrub(%q) = %q, want %q", in, got, want)
		}
	}
}
