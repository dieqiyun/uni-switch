package sessions

import (
	"runtime"
	"sync/atomic"
	"testing"
	"unsafe"
)

// holdsEnd ends, when the test does, every hold it left: one that fails
// before its done() would otherwise have every later test in the package
// read the listing it held (#4177).
func holdsEnd(t *testing.T) {
	t.Cleanup(func() {
		held.Lock()
		held.n, held.lists = 0, nil
		held.Unlock()
	})
}

// Under a hold a folder is listed once; a hold begun after a listing has
// it listed again, so no request reads files from before it came; with
// no hold left every read lists.
func TestOneListing(t *testing.T) {
	holdsEnd(t)
	n := 0
	list := func() []file { n++; return []file{{path: "a"}} }
	read := func() {
		if fs := heldFiles("k", list); len(fs) != 1 || fs[0].path != "a" {
			t.Fatalf("heldFiles = %v", fs)
		}
	}
	read()
	read()
	if n != 2 {
		t.Fatalf("listed %d times with no hold, want 2", n)
	}
	n = 0
	done := OneListing()
	read()
	read()
	if n != 1 {
		t.Fatalf("listed %d times under one hold, want 1", n)
	}
	other := OneListing()
	read()
	if n != 2 {
		t.Fatalf("a hold begun after the listing read it: listed %d times, want 2", n)
	}
	read()
	done()
	read()
	if n != 2 {
		t.Fatalf("listed %d times while a hold was left, want 2", n)
	}
	other()
	other()
	read()
	if n != 3 {
		t.Fatalf("listed %d times after the holds ended, want 3", n)
	}
	if held.n != 0 || held.lists != nil {
		t.Fatalf("holds left: %d, lists %v", held.n, held.lists)
	}
}

// A read that comes while a folder is being listed under a hold waits for
// that listing; a shared hold reads the listing a hold before it took, and
// begins one of its own when there is none.
func TestShareListing(t *testing.T) {
	holdsEnd(t)
	var n atomic.Int32
	listing, release := make(chan struct{}), make(chan struct{})
	slow := func() []file {
		if n.Add(1) == 1 {
			close(listing)
			<-release
		}
		return []file{{path: "a"}}
	}
	waiting := make(chan struct{}, 1)
	heldWaiting = func() { waiting <- struct{}{} }
	t.Cleanup(func() { heldWaiting = nil })
	done := OneListing()
	got := make(chan []file, 2)
	go func() { got <- heldFiles("k", slow) }()
	select {
	case <-listing:
	case <-got:
		t.Fatal("a hold's first read didn't list: a listing was left from before it")
	}
	shared := ShareListing()
	go func() { got <- heldFiles("k", slow) }()
	select {
	case <-waiting:
	case <-got:
		t.Fatal("a read during the listing didn't wait for it")
	}
	close(release)
	for range 2 {
		if fs := <-got; len(fs) != 1 || fs[0].path != "a" {
			t.Fatalf("heldFiles = %v", fs)
		}
	}
	if n.Load() != 1 {
		t.Fatalf("listed %d times for a read during the listing and a shared hold, want 1", n.Load())
	}
	done()
	shared()

	n.Store(1) // listing again is quick
	shared = ShareListing()
	heldFiles("k", slow)
	heldFiles("k", slow)
	shared()
	if n.Load() != 2 {
		t.Fatalf("a shared hold with none before it listed %d times, want 1", n.Load()-1)
	}
	if held.n != 0 || held.lists != nil {
		t.Fatalf("holds left: %d, lists %v", held.n, held.lists)
	}
}

// The reads under a hold share one listing, not a copy each (#2278), and
// it is clipped: an append made to it by one reader leaves the others'
// as it was.
func TestHeldListingShared(t *testing.T) {
	holdsEnd(t)
	list := func() []file { return append(make([]file, 0, 8), file{path: "a"}, file{path: "b"}) }
	done := OneListing()
	defer done()
	one, two := heldFiles("k", list), heldFiles("k", list)
	if &one[0] != &two[0] {
		t.Fatal("each read under the hold got a copy of the listing")
	}
	_ = append(one, file{path: "c"})
	if three := heldFiles("k", list); len(three) != 2 || three[:cap(three)][len(three)-1].path != "b" {
		t.Fatalf("an append to one read's listing reached another's: %v", three[:cap(three)])
	}
}

// A listing set or buffer given back holds nothing of the pass before.
func TestListingPoolsGiveBackEmpty(t *testing.T) {
	for range 3 {
		got := distinctOf([]file{{path: "a"}, {path: "b"}}, []file{{path: "a"}, {path: "c"}})
		if len(got) != 3 || got[0].path != "a" || got[1].path != "b" || got[2].path != "c" {
			t.Fatalf("distinctOf = %v", got)
		}
	}
	fs := append(listingBuf(), file{path: "x", agent: "codex"})
	putListingBuf(fs)
	if b := listingBuf(); len(b) != 0 || (cap(b) > 0 && b[:1][0].path != "") {
		t.Fatalf("a listing given back kept its files: %v", b[:cap(b)])
	}
}

// A listing or path set given back is kept across garbage collections: a
// poll collects about twice a second, and the sync.Pools they were in,
// emptied by each, had none for most reads (#2278).
func TestListingPoolsKeptAcrossCollections(t *testing.T) {
	for ok := true; ok; _, ok = listingBufs.take(nil) {
	}
	for ok := true; ok; _, ok = pathSets.take(nil) {
	}
	fs := append(listingBuf(), make([]file, 64)...)
	room := unsafe.SliceData(fs)
	putListingBuf(fs)
	m := pathSet()
	m["a"] = struct{}{}
	putPathSet(m)
	runtime.GC()
	runtime.GC()
	if b := listingBuf(); cap(b) == 0 || unsafe.SliceData(b[:1]) != room {
		t.Error("a listing given back was let go by a garbage collection")
	} else {
		putListingBuf(b)
	}
	got := pathSet()
	defer putPathSet(got)
	if len(got) != 0 {
		t.Errorf("a path set given back kept its paths: %v", got)
	}
	got["b"] = struct{}{}
	if _, ok := m["b"]; !ok {
		t.Error("a path set given back was let go by a garbage collection")
	}
}
