package sessions

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// A save right after another waits out saveGap rather than writing the
// whole index again (each Sessions poll wrote it), and Saved writes it at
// once, not after the gap.
func TestSaveWaitsOutTheGap(t *testing.T) {
	setup(t)
	var writes atomic.Int32
	old := saveGap
	saveGap = time.Hour
	saveHook = func() { writes.Add(1) }
	t.Cleanup(func() { saveGap, saveHook = old, nil })
	saved()
	saving.Lock()
	saving.wrote = time.Time{}
	saving.Unlock()

	saveCache()
	saved()
	if n := writes.Load(); n != 1 {
		t.Fatalf("%d writes of the first save, want 1", n)
	}
	saveCache()
	saveCache()
	time.Sleep(50 * time.Millisecond) // room for a write the gap should hold back
	if n := writes.Load(); n != 1 {
		t.Fatalf("%d writes with saveGap not yet past, want the first only", n)
	}
	done := make(chan struct{})
	go func() { saved(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Saved waited out the gap")
	}
	if n := writes.Load(); n != 2 {
		t.Fatalf("%d writes once Saved, want the two saves in a row folded into one", n)
	}
}

// A scan's last save is written at once, not saveGap after the save before
// it: a uni-switch quit or updated within that minute kept none of the scan,
// and each start read 22 GB of rollouts again. A poll's few files still
// wait out the gap.
func TestScanSavedWithoutTheGap(t *testing.T) {
	inZone(t, 0)
	claude, _ := setup(t)
	var writes atomic.Int32
	old, oldScan := saveGap, scanFiles
	saveGap, scanFiles = time.Hour, 8
	saveHook = func() { writes.Add(1) }
	t.Cleanup(func() { saved(); saveGap, scanFiles, saveHook = old, oldScan, nil })
	statsAt(0, statsNow)
	saved()
	gapFromNow := func() {
		saving.Lock()
		saving.wrote = time.Now()
		saving.Unlock()
		writes.Store(0)
	}

	gapFromNow()
	moreSessions(t, claude, 12)
	statsAt(0, statsNow)
	for end := time.Now().Add(10 * time.Second); writes.Load() == 0; {
		if time.Now().After(end) {
			t.Fatal("a scan's last save waited out saveGap")
		}
		time.Sleep(10 * time.Millisecond)
	}

	saved()
	gapFromNow()
	p := filepath.Join(claude, "projects", "-work-many", "33333333-2222-3333-4444-000000000000.jsonl")
	if err := os.WriteFile(p, []byte(ccMsg("m0", "2026-09-21T10:00:00.000Z", 200, 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	statsAt(0, statsNow)
	time.Sleep(100 * time.Millisecond) // room for a write the gap should hold back
	if n := writes.Load(); n != 0 {
		t.Fatalf("a poll's save written %d times inside saveGap", n)
	}
}
