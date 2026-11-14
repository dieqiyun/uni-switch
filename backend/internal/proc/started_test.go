package proc

import (
	"os"
	"runtime"
	"testing"
	"time"
)

// initAt is read as the test binary initialises its packages, moments
// after the system recorded its start.
var initAt = time.Now()

// Started reads this process's start as the system recorded it: before
// its package init ran, and not long before (a loaded machine is slow to
// start a binary). A pid that isn't running errs.
func TestStarted(t *testing.T) {
	got, err := Started(os.Getpid())
	switch runtime.GOOS {
	case "darwin", "linux", "windows":
	default:
		if err != ErrNoStart {
			t.Fatalf("on %s: %v %v, want ErrNoStart", runtime.GOOS, got, err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	// Linux's btime is whole seconds
	if got.After(initAt.Add(time.Second)) || got.Before(initAt.Add(-time.Minute)) {
		t.Fatalf("started %v, package init at %v", got, initAt)
	}
	if _, err := Started(1 << 30); err == nil {
		t.Fatal("a pid that isn't running: no error")
	}
}
