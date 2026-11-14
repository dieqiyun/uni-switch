package usage

import (
	"testing"
	"time"
)

// Queries reading one session file at once make one chunk of it, each
// handed a copy of its own: at a cold start with the app open, the
// warm-up's page and the heatmap each read every file (#2278). A make
// that panics leaves the waiter to make its own.
func TestSharedBuildMakesAFileOnce(t *testing.T) {
	t.Cleanup(func() { buildJoined = nil })
	joined := make(chan struct{}, 1)
	buildJoined = func() { joined <- struct{}{} }
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	made := func() *rowChunk {
		var c rowChunk
		for i := 0; i < logBlockRows; i++ {
			c.add(Row{Record: Record{Time: at, Provider: "openai", Model: "gpt-5", Input: i}}, "", int64(i), false)
		}
		return c.pack()
	}
	k := chunkBuildKey{path: "/s/rollout.jsonl", size: 10, modified: 1}
	builds := 0
	inside, release := make(chan struct{}), make(chan struct{})
	first := make(chan *rowChunk)
	go func() {
		first <- sharedBuild(k, func() *rowChunk {
			builds++
			close(inside)
			<-release
			return made()
		})
	}()
	<-inside
	second := make(chan *rowChunk)
	go func() {
		second <- sharedBuild(k, func() *rowChunk { builds++; return made() })
	}()
	select { // the second waits for the first's chunk
	case <-joined:
	case <-second:
		t.Fatal("a file made again while another query was making it")
	}
	close(release)
	a, b := <-first, <-second
	if builds != 1 {
		t.Fatalf("one file made %d times", builds)
	}
	if a == b || len(a.Archive) == 0 || &a.Archive[0] != &b.Archive[0] {
		t.Fatalf("copies %p and %p of one archive (%d bytes)", a, b, len(a.Archive))
	}
	a.serial, a.Archive = 7, nil
	if b.serial == 7 || len(b.Archive) == 0 || b.unpack().row(3).Input != 3 {
		t.Fatal("one query's copy changed under another's")
	}
	chunkBuilds.Lock()
	n := len(chunkBuilds.m)
	chunkBuilds.Unlock()
	if n != 0 {
		t.Fatalf("%d builds left after both were done", n)
	}

	// a make that panics: the one waiting makes its own
	inside, release = make(chan struct{}), make(chan struct{})
	go func() {
		defer func() { recover() }()
		sharedBuild(k, func() *rowChunk { close(inside); <-release; panic("read") })
	}()
	<-inside
	go func() { second <- sharedBuild(k, made) }()
	<-joined
	close(release)
	if c := <-second; c == nil || c.unpack().row(5).Input != 5 {
		t.Fatalf("after a panicked make, read %v", c)
	}
}
