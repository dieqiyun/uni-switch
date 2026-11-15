package gateway

import (
	"testing"
	"time"
)

// A reply that fills the segment before the request reads it doesn't hold
// the run: the request's own heard, and so its reading, still go on (CI on
// 8fbd39f8: TestClaudeLastWordsAreRead hung 20m on macOS -race, emit
// blocked on a full segment with the run's lock held, heard waiting for it).
func TestEmitOnFullSegmentLeavesTheRunFree(t *testing.T) {
	r := &subscriptionRun{}
	ch := r.attach()
	const n = 200
	go func() {
		for i := 0; i < n; i++ {
			r.emit(Event{Kind: KText, Text: "x"})
		}
		r.endSegment()
	}()
	time.Sleep(50 * time.Millisecond) // the segment is full by now
	heard := make(chan struct{})
	go func() { r.heard(nil); close(heard) }()
	select {
	case <-heard:
	case <-time.After(5 * time.Second):
		t.Fatal("heard waited on a send to a segment no one reads yet")
	}
	got := 0
	for range ch {
		got++
	}
	if got != n {
		t.Fatalf("read %d events of %d", got, n)
	}
}
