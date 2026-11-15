package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/applog"
)

// logOut runs `uni-switch log` with args and returns what it printed.
func logOut(t *testing.T, args ...string) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() { b, _ := io.ReadAll(r); done <- string(b) }()
	err = run(append([]string{"log"}, args...))
	w.Close()
	os.Stdout = old
	out := <-done
	r.Close()
	return out, err
}

func writeLog(t *testing.T, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(applog.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(applog.Path(), []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(applog.Path()) })
}

// `uni-switch log` says where the app's log is, there or not yet; --path
// prints the path alone, for a script or a reply to paste it in.
func TestLogSaysWhere(t *testing.T) {
	os.Remove(applog.Path())
	out, err := logOut(t)
	if err != nil || !strings.HasPrefix(out, applog.Path()+"\n") || !strings.Contains(out, "nothing yet") {
		t.Fatalf("uni-switch log, no log yet: %v\n%s", err, out)
	}
	writeLog(t, "one\n")
	if out, err = logOut(t); err != nil || !strings.Contains(out, "Settings › About › Log") {
		t.Errorf("uni-switch log: %v\n%s", err, out)
	}
	if out, err = logOut(t, "--path"); err != nil || out != applog.Path()+"\n" {
		t.Errorf("uni-switch log --path: %v, %q, want the path alone", err, out)
	}
}

// -n prints the app log's last lines.
func TestLogLastLines(t *testing.T) {
	writeLog(t, "one\ntwo\nthree\nfour\n")
	for n, want := range map[string]string{"2": "three\nfour\n", "0": "", "9": "one\ntwo\nthree\nfour\n"} {
		if out, err := logOut(t, "-n", n); err != nil || out != want {
			t.Errorf("uni-switch log -n %s: %v, %q, want %q", n, err, out, want)
		}
	}
	os.Remove(applog.Path())
	if _, err := logOut(t, "-n", "5"); err == nil || !strings.Contains(err.Error(), "isn't there yet") {
		t.Errorf("uni-switch log -n with no log: %v", err)
	}
}

// A flag or value it doesn't know is refused, never taken for something.
func TestLogRefusesWhatItDoesntKnow(t *testing.T) {
	for _, args := range [][]string{{"--bogus"}, {"-n"}, {"-n", "many"}, {"-n", "-3"}, {"--path", "-f"}, {"app.log"}} {
		if out, err := logOut(t, args...); err == nil {
			t.Errorf("uni-switch log %v: no error\n%s", args, out)
		}
	}
	if !strings.Contains(helpFor([]string{"log", "--help"}), "usage: uni-switch log") {
		t.Errorf("uni-switch log --help: %q", helpFor([]string{"log", "--help"}))
	}
}

// -f prints what is written after, and a log rotated meanwhile is read
// from its start.
func TestLogFollows(t *testing.T) {
	writeLog(t, "old\n")
	var mu syncBuf
	stop := make(chan struct{})
	done := make(chan error)
	go func() { done <- tailLog(&mu, applog.Path(), 1, true, stop) }()
	until(t, &mu, "old\n")
	f, err := os.OpenFile(applog.Path(), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("appended\n")
	f.Close()
	until(t, &mu, "old\nappended\n")
	os.Rename(applog.Path(), filepath.Join(filepath.Dir(applog.Path()), applog.OldName))
	t.Cleanup(func() { os.Remove(filepath.Join(filepath.Dir(applog.Path()), applog.OldName)) })
	os.WriteFile(applog.Path(), []byte("rotated\n"), 0o600)
	until(t, &mu, "old\nappended\nrotated\n")
	close(stop)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// until waits for b to read want, the state the test is about.
func until(t *testing.T, b *syncBuf, want string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for b.String() != want {
		if time.Now().After(deadline) {
			t.Fatalf("followed %q, want %q", b.String(), want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
