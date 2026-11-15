package usage

import (
	"iter"
	"math/bits"
)

// refSet is a set of rows, a bit a row of each chunk it holds. As a
// map[rowRef]bool a row was ~40 bytes, and a cold start pairing the
// whole history holds a set of most of its rows several times over; as
// bits a million rows are 125 KB (#2278).
type refSet map[*rowChunk][]uint64

func (s refSet) has(r rowRef) bool {
	b := s[r.Chunk]
	w := r.Index >> 6
	return w < len(b) && b[w]&(1<<(r.Index&63)) != 0
}

func (s refSet) add(r rowRef) {
	b := s[r.Chunk]
	w := r.Index >> 6
	if w >= len(b) {
		grown := make([]uint64, max(w+1, 2*len(b), (r.Chunk.Count+63)>>6, (len(r.Chunk.Rows)+63)>>6))
		copy(grown, b)
		b = grown
		s[r.Chunk] = b
	}
	b[w] |= 1 << (r.Index & 63)
}

// count is how many rows s holds.
func (s refSet) count() int {
	n := 0
	for _, b := range s {
		for _, w := range b {
			n += bits.OnesCount64(w)
		}
	}
	return n
}

// all is each row s holds, in no order but each chunk's.
func (s refSet) all() iter.Seq[rowRef] {
	return func(yield func(rowRef) bool) {
		for c, b := range s {
			for w, word := range b {
				for word != 0 {
					i := bits.TrailingZeros64(word)
					word &= word - 1
					if !yield(rowRef{c, w<<6 + i}) {
						return
					}
				}
			}
		}
	}
}
