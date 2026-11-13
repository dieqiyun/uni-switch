package library

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// #2321: a probe's download goes when it is replaced or expires, not only
// at the next probe, which may never come.
func TestProbeDownloadEndsWithIt(t *testing.T) {
	t.Cleanup(forgetProbes)
	defer func(l time.Duration) { probeLife = l }(probeLife)
	probeLife = time.Hour
	first, second := t.TempDir(), t.TempDir()
	keepProbe("owner/repo", &Probe{}, first)
	keepProbe("owner/repo", &Probe{}, second)
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("the replaced probe's download is still there: %v", err)
	}
	if _, err := os.Stat(second); err != nil {
		t.Fatalf("the probe kept lost its download: %v", err)
	}

	probeLife = 20 * time.Millisecond
	third := t.TempDir()
	keepProbe("owner/other", &Probe{}, third)
	deadline := time.Now().Add(time.Minute)
	for {
		probes.Lock()
		_, cached := probes.m["owner/other"]
		probes.Unlock()
		_, err := os.Stat(third)
		if !cached && os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("an expired probe is kept: cached %v, download %v", cached, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// #2321: a download a uni-switch that quit left behind goes at the next probe;
// one that may be in use, and what isn't ours, stays.
func TestStaleDownloadsSwept(t *testing.T) {
	dir := t.TempDir()
	for _, k := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(k, dir)
	}
	if os.TempDir() != dir {
		t.Fatalf("the temp dir is %s, not %s", os.TempDir(), dir)
	}
	old := filepath.Join(dir, skillTemp+"1")
	fresh := filepath.Join(dir, skillTemp+"2")
	other := filepath.Join(dir, "someone-else-1")
	for _, d := range []string{old, fresh, other} {
		if err := os.MkdirAll(filepath.Join(d, "skills"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	long := time.Now().Add(-2 * time.Hour)
	for _, d := range []string{old, other} {
		if err := os.Chtimes(d, long, long); err != nil {
			t.Fatal(err)
		}
	}
	sweepStaleDownloads()
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("a download two hours old is still there: %v", err)
	}
	for _, d := range []string{fresh, other} {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("%s was swept: %v", filepath.Base(d), err)
		}
	}
}
