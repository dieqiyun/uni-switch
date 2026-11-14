package sessions

import (
	"bytes"
	"math/rand/v2"
	"strings"
	"testing"
)

// lastIndex answers what bytes.LastIndex does: in the tail, before it, across
// its edge, with keys that overlap themselves, and with none.
func TestLastIndexIsLastIndex(t *testing.T) {
	keys := [][]byte{[]byte(`"timestamp":"`), []byte(`"usage":{`), []byte("aa"), []byte("a"), nil}
	rnd := rand.New(rand.NewPCG(1, 2))
	for n := range 3000 {
		size := rnd.IntN(3000)
		b := make([]byte, size)
		for i := range b {
			b[i] = "ab\"x"[rnd.IntN(4)]
		}
		key := keys[n%len(keys)]
		for range rnd.IntN(3) {
			if len(key) <= size {
				copy(b[rnd.IntN(size-len(key)+1):], key)
			}
		}
		if got, want := lastIndex(b, key), bytes.LastIndex(b, key); got != want {
			t.Fatalf("lastIndex(%d bytes, %q) = %d, want %d", size, key, got, want)
		}
	}
	key := []byte(`"timestamp":"`)
	for _, at := range []int{0, 500, 2048 - 1024 - 5, 2048 - 1024, 2048 - len(key)} {
		b := []byte(strings.Repeat("x", 2048))
		copy(b[at:], key)
		if got := lastIndex(b, key); got != at {
			t.Errorf("key at %d: lastIndex = %d", at, got)
		}
		copy(b[10:], key)
		if got := lastIndex(b, key); got != max(at, 10) {
			t.Errorf("keys at 10 and %d: lastIndex = %d", at, got)
		}
	}
}
