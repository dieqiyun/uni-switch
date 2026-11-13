package sessions

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// The warm-up at start (Warm) reads every changed session file before any
// page asks for them. After an update that changes how a file is read, that
// is every file again: 8.9 GB of Claude Code and Codex sessions took ~40 s
// of CPU, and with twice as many readers as cores it held every core for
// the first ~15 s of uni-switch's start (#1665, laowangba233: the CPU full on
// each start). Nobody waits on the warm-up, so it reads with a quarter of
// the cores; a page that asks for sessions meanwhile (Ask) has the reading
// go at full speed again, as it then waits on it.

var (
	warming, asking atomic.Int32
	quietSlots      = make(chan struct{}, quietReaders())
)

// quietReaders is how many files the warm-up reads at once.
func quietReaders() int { return max(1, runtime.NumCPU()/4) }

// scanReaders is how many files the index reads at once with a page
// waiting. It was twice the cores, up to 32, as if the reading waited on
// the disk; it doesn't on an SSD, decoding and parsing is the work, and a
// first read of 15 GB of Codex sessions held 22 of 32 cores (2154%) while
// the page waited, the machine's other apps slowed beside it (#3238). Half
// the cores leaves the other half to them.
func scanReaders() int { return max(2, runtime.NumCPU()/2) }

// Warm runs fn, the reads nobody waits on yet, with few readers.
func Warm(fn func()) {
	warming.Add(1)
	defer func() {
		warming.Add(-1)
		forgetWarmListing()
	}()
	fn()
}

// Ask marks a page waiting on sessions until done is called: reading goes
// at full speed while it waits.
func Ask() (done func()) {
	asking.Add(1)
	return sync.OnceFunc(func() { asking.Add(-1) })
}

func quiet() bool { return warming.Load() > 0 && asking.Load() == 0 }

// readSlot waits, while only the warm-up reads, for one of the warm-up's
// few reading slots; release gives it back. A page asking meanwhile lets
// the reader go at once.
func readSlot() (release func()) {
	if !quiet() {
		return func() {}
	}
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	for quiet() {
		select {
		case quietSlots <- struct{}{}:
			return sync.OnceFunc(func() { <-quietSlots })
		case <-t.C:
		}
	}
	return func() {}
}

// reading and readPeak count files being read at once, for a test.
var reading, readPeak atomic.Int32

func readBegin() (end func()) {
	n := reading.Add(1)
	for p := readPeak.Load(); n > p && !readPeak.CompareAndSwap(p, n); p = readPeak.Load() {
	}
	return func() { reading.Add(-1) }
}
