package sessions

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// moreSessions adds n Claude Code sessions to setup's.
func moreSessions(t *testing.T, claude string, n int) {
	t.Helper()
	dir := filepath.Join(claude, "projects", "-work-many")
	os.MkdirAll(dir, 0o755)
	for i := range n {
		p := filepath.Join(dir, fmt.Sprintf("33333333-2222-3333-4444-%012d.jsonl", i))
		os.WriteFile(p, []byte(ccMsg(fmt.Sprintf("m%d", i), "2026-09-21T10:00:00.000Z", 100, 10)), 0o644)
	}
}

// holdQuietSlots takes every one of the warm-up's reading slots, and gives
// them back when the test ends.
func holdQuietSlots(t *testing.T) {
	for range cap(quietSlots) {
		quietSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for len(quietSlots) > 0 {
			<-quietSlots
		}
	})
}

// The warm-up at start reads every changed file before anyone asks for
// them; after an update that reads them all again it held every core
// (#1665). It reads only with the warm-up's few slots: with all of them
// taken, neither the index (Stats) nor the calls (ReadCallSource) read a
// file, and a page asking has them read at once.
func TestWarmReadsOnlyInItsSlots(t *testing.T) {
	inZone(t, 0)
	claude, _ := setup(t)
	moreSessions(t, claude, 12)
	holdQuietSlots(t)
	warm := func(what string, read func()) {
		t.Helper()
		readPeak.Store(0)
		done := make(chan struct{})
		go Warm(func() { read(); close(done) })
		time.Sleep(300 * time.Millisecond)
		if n := readPeak.Load(); n != 0 {
			t.Fatalf("%s: the warm-up read %d files at once with no slot free", what, n)
		}
		// a page asks: read at full speed, slots or not
		asked := Ask()
		defer asked()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Fatalf("%s: a page asking didn't have the warm-up's reading go on", what)
		}
		if readPeak.Load() == 0 {
			t.Fatalf("%s: nothing read", what)
		}
	}
	warm("the index", func() { statsAt(0, statsNow) })
	if got := len(List(0)); got < 12 {
		t.Fatalf("%d sessions listed", got)
	}
	// the index read each file's calls along (callsRide): a line more in
	// each leaves the calls something of their own to read
	for _, s := range CallSources() {
		appendText(t, s.Path, "\n")
	}
	warm("the calls", func() {
		for _, s := range CallSources() {
			ReadCallSource(s)
		}
	})
}

// Outside the warm-up the slots don't hold anything up.
func TestReadSlotOnlyWhileWarming(t *testing.T) {
	holdQuietSlots(t)
	got := make(chan struct{})
	go func() { readSlot()(); close(got) }()
	select {
	case <-got:
	case <-time.After(10 * time.Second):
		t.Fatal("a read outside the warm-up waited for a slot")
	}
}

// A long first read keeps what it has read on disk as it goes: a uni-switch
// quit before the end (its CPU full on a slow disk, #1665) kept nothing of
// a minutes-long read, so the next start read it all again. The files after
// the first three wait until a save is written with files still to read.
func TestLongReadKeepsWhatItRead(t *testing.T) {
	inZone(t, 0)
	claude, _ := setup(t)
	moreSessions(t, claude, 12)
	old := checkpointEvery
	checkpointEvery = 5 * time.Millisecond
	early := make(chan struct{})
	var once sync.Once
	saveHook = func() {
		if progress.files.Load() < progress.todo.Load() {
			once.Do(func() { close(early) })
		}
	}
	var parsed atomic.Int32
	parseHook = func() {
		if parsed.Add(1) > 3 {
			select {
			case <-early:
			case <-time.After(5 * time.Second):
			}
		}
	}
	t.Cleanup(func() { checkpointEvery, saveHook, parseHook = old, nil, nil })

	statsAt(0, statsNow)
	Saved()
	select {
	case <-early:
	default:
		t.Fatal("nothing kept on disk until every file was read")
	}
}

// With a page waiting, the index reads with half the cores, not twice
// them: a first read held 22 of 32 cores beside the user's apps (#3238).
func TestAskedIndexReadsWithHalfTheCores(t *testing.T) {
	inZone(t, 0)
	claude, _ := setup(t)
	moreSessions(t, claude, 4*scanReaders()+8)
	parseHook = func() { time.Sleep(20 * time.Millisecond) }
	t.Cleanup(func() { parseHook = nil })
	readPeak.Store(0)
	defer Ask()()
	statsAt(0, statsNow)
	if n := readPeak.Load(); n > int32(scanReaders()) || n < int32(min(2, scanReaders())) {
		t.Fatalf("the index read %d files at once, want 2 to %d", n, scanReaders())
	}
}
