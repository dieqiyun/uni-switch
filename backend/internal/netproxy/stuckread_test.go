package netproxy

import (
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type readAnswer struct {
	p  Proxy
	ok bool
}

// holdReads makes each read of the system proxy wait for the test's
// answer. The cache starts as p, read at at (zero: nothing read yet), and
// a request waits for a read until wait after it began. It returns how
// many reads began and where to answer them.
func holdReads(t *testing.T, p Proxy, at time.Time, wait time.Duration) (*atomic.Int32, chan<- readAnswer) {
	t.Helper()
	var reads atomic.Int32
	answers, stop := make(chan readAnswer), make(chan struct{})
	oldRead, oldWait := readSystem, readWait
	readSystem = func() (Proxy, bool) {
		reads.Add(1)
		select {
		case a := <-answers:
			return a.p, a.ok
		case <-stop:
			return Proxy{}, false
		}
	}
	readWait = wait
	sysCache.Lock()
	sysCache.at, sysCache.p, sysCache.failed = at, p, false
	sysCache.Unlock()
	t.Cleanup(func() {
		// the read still under way ends before the next test reads
		close(stop)
		readEnds(t, underWay())
		readSystem, readWait = oldRead, oldWait
		sysCache.Lock()
		sysCache.at, sysCache.p, sysCache.failed = time.Time{}, Proxy{}, false
		sysCache.Unlock()
	})
	return &reads, answers
}

// underWay is the read of the system proxy under way, nil when none is.
func underWay() chan struct{} {
	sysCache.Lock()
	defer sysCache.Unlock()
	return sysCache.reading
}

func readEnds(t *testing.T, done chan struct{}) {
	t.Helper()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the read of the system proxy never ended")
	}
}

// ask is System() for a test that fails when no answer comes in 10s.
func ask(t *testing.T) Proxy {
	t.Helper()
	got := make(chan Proxy, 1)
	go func() { got <- System() }()
	select {
	case p := <-got:
		return p
	case <-time.After(10 * time.Second):
		t.Fatal("a request still waits on a read of the system proxy that never ends")
		return Proxy{}
	}
}

// #3421: a scutil --proxy that never answered held System's lock, and
// with it every request that asked for the system's proxy, for as long as
// it hung. A request now waits for a read until readWait after the read
// began, then goes on with what was read before (here nothing yet, so
// direct), and one read runs at a time.
func TestStuckSystemProxyReadHoldsNoRequest(t *testing.T) {
	const wait = time.Second
	reads, answers := holdReads(t, Proxy{}, time.Time{}, wait)

	got := make(chan Proxy, 8)
	for range 8 {
		go func() { got <- System() }()
	}
	for i := range 8 {
		select {
		case p := <-got:
			if p.URL != "" {
				t.Fatalf("nothing read yet, but a request went through %+v", p)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%d of 8 requests still wait on a read of the system proxy that never ends", 8-i)
		}
	}
	if n := reads.Load(); n != 1 {
		t.Fatalf("%d reads of the system proxy at once, want 1", n)
	}

	// The read is past readWait: a request doesn't wait for it again.
	start := time.Now()
	ask(t)
	if took := time.Since(start); took >= wait {
		t.Fatalf("a request waited %v for a read already past readWait (%v)", took, wait)
	}

	// It answers at last, and what it read is taken.
	done := underWay()
	answers <- readAnswer{Proxy{URL: "http://127.0.0.1:7890"}, true}
	readEnds(t, done)
	if p := ask(t); p.URL != "http://127.0.0.1:7890" {
		t.Fatalf("the read answered, but requests take %+v", p)
	}
	if n := reads.Load(); n != 1 {
		t.Fatalf("%d reads of the system proxy, want 1", n)
	}
}

// A read that fails, or hasn't answered by readWait, leaves the proxy
// read before in place, so requests keep going through it. The read after
// a failed one holds nobody.
func TestFailedSystemProxyReadKeepsTheProxy(t *testing.T) {
	const wait = time.Second
	set := Proxy{URL: "http://127.0.0.1:7890"}
	reads, answers := holdReads(t, set, time.Now().Add(-time.Hour), wait)

	if p := ask(t); p.URL != set.URL {
		t.Fatalf("while a read hangs, requests lost the proxy read before: %+v", p)
	}
	done := underWay()
	answers <- readAnswer{Proxy{}, false}
	readEnds(t, done)
	if p := ask(t); p.URL != set.URL {
		t.Fatalf("a failed read dropped the proxy read before: %+v", p)
	}
	if n := reads.Load(); n != 1 {
		t.Fatalf("%d reads: a failed one was tried again before its keep was up", n)
	}

	// Its keep is up: the next read is tried, and no request waits for it.
	sysCache.Lock()
	sysCache.at = time.Now().Add(-time.Hour)
	sysCache.Unlock()
	start := time.Now()
	if p := ask(t); p.URL != set.URL {
		t.Fatalf("after a failed read, requests lost the proxy read before: %+v", p)
	}
	if took := time.Since(start); took >= wait {
		t.Fatalf("a request waited %v for the read after a failed one", took)
	}
	done = underWay()
	answers <- readAnswer{Proxy{URL: "http://127.0.0.1:7891"}, true}
	readEnds(t, done)
	if p := ask(t); p.URL != "http://127.0.0.1:7891" {
		t.Fatalf("the read after a failed one answered, but requests take %+v", p)
	}
}

// The tool that reads the system's proxy (scutil on macOS) is ended at
// toolWait, and its read fails; what a tool that answers prints is parsed.
func TestSystemProxyToolThatHangsIsEnded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no echo or sleep program on Windows")
	}
	parse := func(out string) Proxy { return Proxy{URL: strings.TrimSpace(out)} }
	if p, ok := readTool(parse, "echo", "http://127.0.0.1:7890"); !ok || p.URL != "http://127.0.0.1:7890" {
		t.Fatalf("a tool that answered read as %+v, %v", p, ok)
	}

	old := toolWait
	toolWait = 100 * time.Millisecond
	t.Cleanup(func() { toolWait = old })
	start := time.Now()
	if p, ok := readTool(parse, "sleep", "30"); ok || p.URL != "" {
		t.Fatalf("a tool that never answered read as %+v, %v", p, ok)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("a tool that never answered held the read %v", took)
	}
}

// A read that answers within readWait is what the request waiting for it
// goes through: uni-switch started where only the system's proxy reaches the
// vendors sends its first requests through it, not direct.
func TestQuickSystemProxyReadIsWaitedFor(t *testing.T) {
	_, answers := holdReads(t, Proxy{}, time.Time{}, 5*time.Second)
	got := make(chan Proxy, 1)
	go func() { got <- System() }()
	answers <- readAnswer{Proxy{URL: "http://127.0.0.1:7890"}, true}
	select {
	case p := <-got:
		if p.URL != "http://127.0.0.1:7890" {
			t.Fatalf("the first read answered at once, but the request waiting for it took %+v", p)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a request still waits on a read that answered")
	}
}
