package proc

import (
	"os/exec"
	"runtime"
	"testing"
	"time"
)

// A running tree's resident memory is read, and none once it was waited
// for: its pid may be another process's by then.
func TestTreeRSS(t *testing.T) {
	name, args := "sleep", []string{"30"}
	if runtime.GOOS == "windows" {
		name, args = "ping", []string{"-n", "30", "127.0.0.1"}
	}
	if _, err := exec.LookPath(name); err != nil {
		t.Skip("no " + name)
	}
	tree, err := StartTree(exec.Command(name, args...))
	if err != nil {
		t.Fatal(err)
	}
	// just after its exec a command holds a few pages, until its loader
	// maps what it runs on (16KiB on ubuntu CI), so it is read until it
	// does; under WSL2 the first read can say 0 kB, which isn't a reading
	// (#4033)
	rss, ok := tree.RSS()
	for end := time.Now().Add(10 * time.Second); (!ok || rss < 64<<10) && time.Now().Before(end); {
		time.Sleep(20 * time.Millisecond)
		rss, ok = tree.RSS()
	}
	if !ok || rss < 64<<10 || rss > 1<<30 {
		t.Fatalf("a running %s holds %d bytes (%v)", name, rss, ok)
	}
	tree.Kill()
	_ = tree.Wait()
	if _, ok := tree.RSS(); ok {
		t.Fatal("a tree waited for was read")
	}
	if MemTotal() <= 0 && (runtime.GOOS == "darwin" || runtime.GOOS == "linux" || runtime.GOOS == "windows") {
		t.Fatal("this computer's memory isn't known")
	}
}
