package agent

import (
	"reflect"
	"slices"
	"strconv"
	"testing"
)

// A look that only reads a kept list, or appends it onto one of its own,
// shares the list: a copy for each look was 42 MB of what twenty window
// polls allocated. get still hands each caller a copy of its own, and an
// append onto the shared list never writes into what is kept.
func TestKeptListSharedWithoutACopy(t *testing.T) {
	var s stampedLists[Option]
	build := func() []Option {
		out := make([]Option, 500)
		for i := range out {
			out[i] = Option{Value: "p/m" + strconv.Itoa(i), Note: "via uni-switch"}
		}
		return out
	}
	want := slices.Clone(s.shared("k", build))
	for range 20 {
		// the kept array itself, not a copy of it (~100 KB for 500); a
		// change of the stamp while the package's tests run builds it
		// again, and that one is kept
		got := s.shared("k", build)
		s.Lock()
		kept := s.m["k"].list
		s.Unlock()
		if &got[0] != &kept[0] {
			t.Fatal("a shared look copies the kept list")
		}
	}
	grown := append(s.shared("k", build), Option{Value: "mine"})
	grown[0].Value = "changed"
	own := s.get("k", build)
	own[1].Value = "changed too"
	if got := s.shared("k", build); !reflect.DeepEqual(got, want) {
		t.Fatalf("the kept list was written into: %q, %q", got[0].Value, got[1].Value)
	}
}
