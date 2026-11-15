//go:build !windows

package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeOpen puts an `open` first on PATH that fails its first fails tries
// as LaunchServices does while it still holds the old uni-switch as running
// ("-600 procNotFound"), then writes its arguments to dir/opened, -n
// included. Each try is counted in dir/tries.
func fakeOpen(t *testing.T, fails int) string {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
d='` + dir + `'
n=$(( $(cat "$d/tries" 2>/dev/null || echo 0) + 1 ))
echo $n > "$d/tries"
app=
for a in "$@"; do case "$a" in *.app) app=$a;; esac; done
if [ $n -le ` + strconv.Itoa(fails) + ` ]; then
  echo "_LSOpenURLsWithCompletionHandler() failed for the application $app with error -600." >&2
  exit 1
fi
printf '%s\n' "$*" > "$d/opened"
`
	if err := os.WriteFile(filepath.Join(dir, "open"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

// gonePid is the pid of a process that has exited, as uni-switch has when the
// relaunch's open runs.
func gonePid(t *testing.T) int {
	t.Helper()
	c := exec.Command("/bin/sh", "-c", "exit 0")
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	return c.Process.Pid
}

func tries(dir string) int {
	b, _ := os.ReadFile(filepath.Join(dir, "tries"))
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

// The report: uni-switch updated itself and exited, and open of the new
// bundle 160ms later failed with -600, LaunchServices still sending to
// the process that had exited. open ran once, so uni-switch stayed closed
// until the user started it nine hours later. It is tried again.
func TestRelaunchOpenTriedAgain(t *testing.T) {
	dir := fakeOpen(t, 2)
	bundle := filepath.Join(t.TempDir(), "uni-switch.app")
	log := filepath.Join(t.TempDir(), relaunchLogName)
	if err := runAfterExit(gonePid(t), relaunchCommand(bundle, false, false, log)); err != nil {
		t.Fatal(err)
	}
	opened := filepath.Join(dir, "opened")
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if b, err := os.ReadFile(opened); err == nil && len(b) > 0 {
			// -n: a new instance, not a reopen sent to the gone one; -g: in the
			// background, as only the menu bar icon comes back
			if got, want := strings.TrimSpace(string(b)), "-n -g "+bundle+" --args tray"; got != want {
				t.Fatalf("opened %q, want %q", got, want)
			}
			if n := tries(dir); n != 3 {
				t.Fatalf("open tried %d times, want 3 (two refusals, then the open)", n)
			}
			// open writes opened before the shell writes its note for try
			// 3, the shell's last write; wait for it, or the TempDir
			// cleanup can race that note while the shell this test started
			// is still going (#3881: TempDir RemoveAll cleanup: unlinkat
			// .../003: directory not empty).
			waitForNotes(t, log, 3)
			return
		}
	}
	t.Fatalf("open was tried %d times and the new version never opened", tries(dir))
}

// waitForNotes waits for the relaunch's shell to have written its n-th
// "open try" note to the log: once an open went through, that note is the
// shell's last write, and after it the shell only exits.
func waitForNotes(t *testing.T, log string, n int) {
	t.Helper()
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if b, err := os.ReadFile(log); err == nil && strings.Count(string(b), "open try ") >= n {
			return
		}
	}
	t.Fatalf("after 30s the relaunch's shell never wrote %d try notes to the log", n)
}

// runRetried runs retried's shell over the fake open, failing fails
// times, to its end, and says how often open ran and how it exited.
func runRetried(t *testing.T, fails int, waits []string) (int, error) {
	t.Helper()
	dir := fakeOpen(t, fails)
	c := exec.Command("/bin/sh", "-c", retried(`open "x.app"`, waits))
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case err := <-done:
		return tries(dir), err
	case <-time.After(30 * time.Second):
		c.Process.Kill()
		<-done
		t.Fatal("the shell was still trying after 30s")
	}
	return 0, nil
}

// An open that never works is given up on after the last wait, not tried
// forever by a shell nobody sees; one that works is not run again.
func TestRelaunchOpenGivesUp(t *testing.T) {
	waits := []string{"0.1", "0.1", "0.1"}
	n, err := runRetried(t, 1000, waits)
	if err == nil {
		t.Error("never opened, and the shell said it did")
	}
	if n != len(waits)+1 {
		t.Errorf("a refusing open tried %d times, want %d", n, len(waits)+1)
	}
	n, err = runRetried(t, 1, waits)
	if err != nil {
		t.Errorf("opened on the second try, and the shell said %v", err)
	}
	if n != 2 {
		t.Errorf("open tried %d times, want 2: once refused, once opened, then no more", n)
	}
}

// The waits add up to the 10 to 15 seconds a LaunchServices that hasn't
// let go of the old process is given.
func TestOpenWaitsTotal(t *testing.T) {
	var sum float64
	for _, w := range openWaits {
		f, err := strconv.ParseFloat(w, 64)
		if err != nil {
			t.Fatalf("wait %q: %v", w, err)
		}
		sum += f
	}
	if sum < 10 || sum > 15 {
		t.Fatalf("waits add to %.1fs, want 10 to 15", sum)
	}
}
