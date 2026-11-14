package sessions

import (
	"bytes"
	"runtime"
	"testing"
	"time"
)

// A cold start collects about once a second, and a sync.Pool is empty two
// collections later: what the scans give back must still be there.
func TestLineReaderKeptAcrossCollections(t *testing.T) {
	emptyShelf(&lineShelves[len(lineShelves)-1])
	r, release := lineReader(bytes.NewReader(nil), 1<<20)
	release()
	runtime.GC()
	runtime.GC()
	runtime.GC()
	again, release := lineReader(bytes.NewReader(nil), 1<<20)
	defer release()
	if again != r {
		t.Fatal("a 1 MiB line reader was made again after three collections")
	}
}

func TestLongLineKeptAcrossCollections(t *testing.T) {
	emptyShelf(&longShelf)
	p, b := takeLong(2 << 20)
	giveLong(p, b)
	runtime.GC()
	runtime.GC()
	runtime.GC()
	again, b := takeLong(1 << 20)
	defer giveLong(again, b)
	if again != p {
		t.Fatal("a 2 MiB long-line buffer was made again after three collections")
	}
}

func TestShelfFullKeepsTheLarger(t *testing.T) {
	var s shelf[int]
	for i := 1; i <= shelfHeld; i++ {
		s.give(i, nil)
	}
	if _, kept, _ := s.give(99, nil); kept {
		t.Fatal("a full shelf kept one more")
	}
	out, kept, swapped := s.give(10, func(h, v int) bool { return h < v })
	if !kept || !swapped || out != 1 {
		t.Fatalf("gave 10 to a full shelf of 1..%d: out %d kept %v swapped %v", shelfHeld, out, kept, swapped)
	}
	if v, ok := s.take(func(v int) bool { return v >= 10 }); !ok || v != 10 {
		t.Fatalf("took %d, %v", v, ok)
	}
}

// An idle uni-switch holds nothing on a shelf.
func TestShelfLetsGoWhenIdle(t *testing.T) {
	s := shelf[int]{idleFor: 20 * time.Millisecond}
	s.give(1, nil)
	deadline := time.Now().Add(10 * time.Second)
	for s.size() > 0 {
		if time.Now().After(deadline) {
			t.Fatal("an idle shelf still holds what it was given")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// and keeps again afterwards
	s.give(2, nil)
	if v, ok := s.take(nil); !ok || v != 2 {
		t.Fatalf("after letting go: took %d, %v", v, ok)
	}
}

// emptyShelf takes what earlier tests' scans left on s.
func emptyShelf[T any](s *shelf[T]) {
	for s.size() > 0 {
		s.take(nil)
	}
}
