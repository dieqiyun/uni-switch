package usage

import (
	"testing"
)

// refMap is s as the map of rows it stood for before it was bits.
func refMap(s refSet) map[rowRef]bool {
	out := map[rowRef]bool{}
	for r := range s.all() {
		out[r] = true
	}
	return out
}

// A set is a bit a row: it holds what was added, past a word's and a
// chunk's end too, and nothing else.
func TestRefSet(t *testing.T) {
	a, b := &rowChunk{Count: 3}, &rowChunk{}
	s := refSet{}
	want := []rowRef{{a, 0}, {a, 2}, {a, 63}, {a, 64}, {a, 700}, {b, 5}}
	for _, r := range want {
		s.add(r)
		s.add(r)
	}
	for _, r := range want {
		if !s.has(r) {
			t.Fatalf("lost %v", r.Index)
		}
	}
	for _, r := range []rowRef{{a, 1}, {a, 65}, {a, 701}, {a, 5000}, {b, 4}, {&rowChunk{}, 0}} {
		if s.has(r) {
			t.Fatalf("holds %v, never added", r.Index)
		}
	}
	if got := s.count(); got != len(want) {
		t.Fatalf("count %d, want %d", got, len(want))
	}
	got := refMap(s)
	if len(got) != len(want) {
		t.Fatalf("all gave %d rows, want %d", len(got), len(want))
	}
	if refSet(nil).has(rowRef{a, 0}) || refSet(nil).count() != 0 {
		t.Fatal("a nil set holds rows")
	}
}

// A match key's sum is the same for the same key and differs when any of
// its parts does, the session's end against the tokens' start too.
func TestMatchKeySum(t *testing.T) {
	k := matchKey{session: "s", tokens: [3]int64{1, 2, 3}, hasID: true}
	if k.sum() != (matchKey{session: "s", tokens: [3]int64{1, 2, 3}, hasID: true}).sum() {
		t.Fatal("one key, two sums")
	}
	for _, o := range []matchKey{
		{session: "t", tokens: [3]int64{1, 2, 3}, hasID: true},
		{session: "s\x01", tokens: [3]int64{0, 2, 3}, hasID: true},
		{session: "s", tokens: [3]int64{1, 2, 4}, hasID: true},
		{session: "s", tokens: [3]int64{1, 2, 3}},
		{session: "s", tokens: [3]int64{1, 2, 3}, hasID: true, failed: true},
	} {
		if o.sum() == k.sum() {
			t.Fatalf("%+v sums as %+v", o, k)
		}
	}
	if n := testing.AllocsPerRun(100, func() { k.sum() }); n != 0 {
		t.Fatalf("a sum allocated %v times", n)
	}
}
