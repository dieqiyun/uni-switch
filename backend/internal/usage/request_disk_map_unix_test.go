//go:build unix

package usage

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

// The chunks a start reads from the file are of the file's pages, not of
// the heap (#3493), and the pages are let go of once no chunk is of them.
func TestRequestDiskChunksMapped(t *testing.T) {
	pageHome(t)
	now := holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local))
	least := requestDiskLeast
	requestDiskLeast = 2
	t.Cleanup(func() { requestDiskLeast = least })
	var unmapped atomic.Int32
	count := func() { unmapped.Add(1) }
	diskUnmapped.Store(&count)
	t.Cleanup(func() { diskUnmapped.Store(nil) })
	dir := filepath.Join(sessions.CodexDir(), "sessions", "2026", "09", "30")
	for i := range 4 {
		writeCodexCall(t, filepath.Join(dir, fmt.Sprintf("rollout-2026-09-30T10-00-0%d-s%d.jsonl", i, i)), fmt.Sprintf("s%d", i), fmt.Sprintf("native-%d", i), `"cli"`, fmt.Sprintf("resp-%d", i), "m", "high", now.Add(-time.Duration(i+1)*time.Hour), 10*(i+1), i)
	}
	forget := func() {
		requestDiskSaves.Wait()
		requestCache.Lock()
		requestCache.chunks = map[string]*rowChunk{}
		requestCache.pages, requestCache.parts, requestCache.pairs, requestCache.heats = nil, nil, nil, nil
		requestCache.key = ""
		requestCache.Unlock()
		sessions.Reset()
	}
	first := queryPage(All, Filter{}, 0, 100, sessions.ReadCallSource)
	forget()
	equalPage(t, queryPage(All, Filter{}, 0, 100, sessions.ReadCallSource), first)

	requestCache.Lock()
	mapped := 0
	for _, c := range requestCache.chunks {
		if len(c.Archive) == 0 {
			continue
		}
		if c.mapped == nil {
			t.Errorf("%s: read from the file into the heap", filepath.Base(c.Source.Path))
			continue
		}
		at, start := uintptr(unsafe.Pointer(unsafe.SliceData(c.Archive))), uintptr(unsafe.Pointer(unsafe.SliceData(c.mapped.data)))
		if at < start || at+uintptr(len(c.Archive)) > start+uintptr(len(c.mapped.data)) {
			t.Errorf("%s: archive outside the mapping", filepath.Base(c.Source.Path))
		}
		mapped++
	}
	requestCache.Unlock()
	if mapped != 4 {
		t.Fatalf("%d chunks of the file's pages, want 4", mapped)
	}

	was := unmapped.Load()
	forget()
	for deadline := time.Now().Add(30 * time.Second); unmapped.Load() == was; {
		if time.Now().After(deadline) {
			t.Fatal("the mapping outlived every chunk of it")
		}
		runtime.GC()
		runtime.Gosched()
	}
}

// The chunks a cold build made are of the file's pages once it is written,
// as a warm start's are, not of the heap until the next start (#3493).
func TestRequestDiskBuiltChunksMapped(t *testing.T) {
	pageHome(t)
	now := holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local))
	least := requestDiskLeast
	requestDiskLeast = 2
	t.Cleanup(func() { requestDiskLeast = least })
	dir := filepath.Join(sessions.CodexDir(), "sessions", "2026", "09", "30")
	for i := range 4 {
		writeCodexCall(t, filepath.Join(dir, fmt.Sprintf("rollout-2026-09-30T10-00-0%d-s%d.jsonl", i, i)), fmt.Sprintf("s%d", i), fmt.Sprintf("native-%d", i), `"cli"`, fmt.Sprintf("resp-%d", i), "m", "high", now.Add(-time.Duration(i+1)*time.Hour), 10*(i+1), i)
	}
	// the write waits while another page copies the chunks with a use of
	// its own, and a third has copied them before the write and keeps
	// them after it
	release, hold, held := make(chan struct{}), make(chan struct{}), make(chan struct{})
	savingDisk = func() { <-release }
	t.Cleanup(func() { savingDisk = nil })
	first := queryPage(All, Filter{}, 0, 100, sessions.ReadCallSource)
	queryPage(All, Filter{}, 1, 100, sessions.ReadCallSource)
	var once sync.Once
	building = func() { once.Do(func() { close(held); <-hold }) }
	t.Cleanup(func() { building = nil })
	done := make(chan struct{})
	go func() {
		defer close(done)
		queryPage(All, Filter{}, 2, 100, sessions.ReadCallSource)
	}()
	<-held
	close(release)
	requestDiskSaves.Wait()
	close(hold)
	<-done

	requestCache.Lock()
	mapped, archived := 0, 0
	for _, c := range requestCache.chunks {
		if len(c.Archive) == 0 {
			continue
		}
		archived++
		if c.mapped == nil {
			t.Errorf("%s: still of the heap after the file was written", filepath.Base(c.Source.Path))
			continue
		}
		at, start := uintptr(unsafe.Pointer(unsafe.SliceData(c.Archive))), uintptr(unsafe.Pointer(unsafe.SliceData(c.mapped.data)))
		if at < start || at+uintptr(len(c.Archive)) > start+uintptr(len(c.mapped.data)) {
			t.Errorf("%s: archive outside the mapping", filepath.Base(c.Source.Path))
		}
		mapped++
	}
	requestCache.Unlock()
	if archived != 4 || mapped != 4 {
		t.Fatalf("%d of %d archived chunks of the file's pages, want 4 of 4", mapped, archived)
	}
	requestCache.Lock()
	requestCache.pages, requestCache.parts, requestCache.pairs, requestCache.heats = nil, nil, nil, nil
	requestCache.key = ""
	requestCache.Unlock()
	equalPage(t, queryPage(All, Filter{}, 0, 100, sessions.ReadCallSource), first)
}

// A file cut short under its mapping by another program is a read that
// failed, not a crash.
func TestRequestDiskMappingCutShort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests-v1.bin")
	if err := os.WriteFile(path, make([]byte, 1<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	m, ok := mapDiskFile(path)
	if !ok {
		t.Fatal("not mapped")
	}
	var last byte
	if !readMapped(m, func() { last = m.data[len(m.data)-1] }) || last != 0 {
		t.Fatal("a whole file's read failed")
	}
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	if readMapped(m, func() { last = m.data[len(m.data)-1] }) {
		t.Fatal("a read past the cut passed")
	}
	runtime.KeepAlive(m)
}

// A large build's archives are of its spill's pages as soon as it is done,
// before requests-v1.bin is written, and of the file's once it is; the
// spill leaves no file behind (#3493).
func TestRequestDiskBuildSpills(t *testing.T) {
	pageHome(t)
	now := holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local))
	least := requestDiskLeast
	requestDiskLeast = 2
	t.Cleanup(func() { requestDiskLeast = least })
	dir := filepath.Join(sessions.CodexDir(), "sessions", "2026", "09", "30")
	for i := range 4 {
		writeCodexCall(t, filepath.Join(dir, fmt.Sprintf("rollout-2026-09-30T10-00-0%d-s%d.jsonl", i, i)), fmt.Sprintf("s%d", i), fmt.Sprintf("native-%d", i), `"cli"`, fmt.Sprintf("resp-%d", i), "m", "high", now.Add(-time.Duration(i+1)*time.Hour), 10*(i+1), i)
	}
	release := make(chan struct{})
	savingDisk = func() { <-release }
	t.Cleanup(func() { savingDisk = nil })
	first := queryPage(All, Filter{}, 0, 100, sessions.ReadCallSource)
	ranks := func() map[int]int {
		requestCache.Lock()
		defer requestCache.Unlock()
		n := map[int]int{}
		for _, c := range requestCache.chunks {
			if len(c.Archive) == 0 {
				continue
			}
			n[c.pagesRank()]++
			if c.mapped == nil {
				continue
			}
			at, start := uintptr(unsafe.Pointer(unsafe.SliceData(c.Archive))), uintptr(unsafe.Pointer(unsafe.SliceData(c.mapped.data)))
			if at < start || at+uintptr(len(c.Archive)) > start+uintptr(len(c.mapped.data)) {
				t.Errorf("%s: archive outside the mapping", filepath.Base(c.Source.Path))
			}
		}
		return n
	}
	if n := ranks(); n[1] != 4 {
		t.Fatalf("before the file is written: %v archived chunks by rank (heap 0, spill 1, file 2), want 4 of the spill", n)
	}
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(requestDiskPath()), "*.spill"))
	if len(left) > 0 {
		t.Errorf("spill files left: %v", left)
	}
	close(release)
	requestDiskSaves.Wait()
	if n := ranks(); n[2] != 4 {
		t.Fatalf("after the file is written: %v archived chunks by rank, want 4 of the file", n)
	}
	requestCache.Lock()
	requestCache.pages, requestCache.parts, requestCache.pairs, requestCache.heats = nil, nil, nil, nil
	requestCache.key = ""
	requestCache.Unlock()
	equalPage(t, queryPage(All, Filter{}, 0, 100, sessions.ReadCallSource), first)
}

// A spill that can't be written has each of its chunks made again: none is
// left without its archive.
func TestArchiveSpillFailedMadeAgain(t *testing.T) {
	pageHome(t)
	sp := newArchiveSpill()
	c := &rowChunk{Archive: []byte("archive")}
	if !sp.put(c, 7) {
		t.Fatal("not put")
	}
	if c.Archive != nil {
		t.Fatal("archive kept on the heap")
	}
	kept, next := *c, *c
	sp.hand(&kept, &next)
	sp.f.Close() // the flush fails
	var made []int
	sp.done(func(i int) *rowChunk {
		made = append(made, i)
		return &rowChunk{Archive: []byte("again")}
	})
	if !slices.Equal(made, []int{7}) {
		t.Fatalf("made again %v, want [7]", made)
	}
	for _, c := range []*rowChunk{&kept, &next} {
		if string(c.Archive) != "again" || c.mapped != nil {
			t.Errorf("archive %q mapped %v, want the one made again, of the heap", c.Archive, c.mapped)
		}
	}
	if _, err := os.Stat(sp.f.Name()); !os.IsNotExist(err) {
		t.Errorf("spill left: %v", err)
	}
}

// The log's full blocks a start reads from log-v1.bin are of the file's
// pages too, not of the heap (#2278).
func TestLogDiskBlocksMapped(t *testing.T) {
	pageHome(t)
	settleLogClock(t)
	t.Cleanup(restartLog)
	restartLog()
	logLines(t, 0, 2*logBlockRows+300)
	readLogSnapshot()
	if _, err := os.Stat(logDiskPath()); err != nil {
		t.Fatalf("the full blocks weren't kept: %v", err)
	}
	restartLog()
	s := readLogSnapshot()
	if len(s.blocks) < 2 {
		t.Fatalf("%d blocks", len(s.blocks))
	}
	for i, c := range s.blocks[:2] {
		if c.mapped == nil {
			t.Fatalf("block %d: read from the file into the heap", i)
		}
		at, start := uintptr(unsafe.Pointer(unsafe.SliceData(c.Archive))), uintptr(unsafe.Pointer(unsafe.SliceData(c.mapped.data)))
		if at < start || at+uintptr(len(c.Archive)) > start+uintptr(len(c.mapped.data)) {
			t.Fatalf("block %d: archive outside the mapping", i)
		}
	}
}
