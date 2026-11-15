package usage

import (
	"slices"
	"unsafe"
)

// codexActors is the Codex thread of each row of a chunk, by row, "" for
// none. A row keeps its thread's place in names, not the string: the
// rows of a day are one thread's turns after another, so a []string held
// a 16-byte header a row for a few hundred names, 24.5 MB of the 184 MB
// live after 34k sessions' start (#2278). A nil ids is no row's.
type codexActors struct {
	ids []uint32
	// names are the threads, names[0] "" when any. A clone shares them
	// clipped, so adding to one never writes into the other's.
	names []string
}

func (a *codexActors) len() int { return len(a.ids) }

// at is row i's thread, "" past the rows it has
func (a *codexActors) at(i int) string {
	if i < len(a.ids) {
		return a.names[a.ids[i]]
	}
	return ""
}

func (a *codexActors) id(actor string) uint32 {
	if len(a.names) == 0 {
		a.names = []string{""}
	}
	if actor == "" {
		return 0
	}
	if n := len(a.ids); n > 0 && a.names[a.ids[n-1]] == actor {
		return a.ids[n-1] // the row before's thread, as most are
	}
	if i := slices.Index(a.names, actor); i >= 0 {
		return uint32(i)
	}
	a.names = append(a.names, actor)
	return uint32(len(a.names) - 1)
}

func (a *codexActors) add(actor string) { a.ids = append(a.ids, a.id(actor)) }

// set makes row i's thread actor, with n rows held
func (a *codexActors) set(n, i int, actor string) {
	if len(a.ids) != n {
		ids := make([]uint32, n)
		copy(ids, a.ids)
		a.ids = ids
	}
	a.ids[i] = a.id(actor)
}

func (a codexActors) clone() codexActors {
	return codexActors{ids: slices.Clone(a.ids), names: slices.Clip(a.names)}
}

// some says some row has a thread
func (a *codexActors) some() bool {
	for _, id := range a.ids {
		if id != 0 {
			return true
		}
	}
	return false
}

func (a codexActors) equal(b codexActors) bool {
	if len(a.ids) != len(b.ids) {
		return false
	}
	for i := range a.ids {
		if a.at(i) != b.at(i) {
			return false
		}
	}
	return true
}

func (a *codexActors) bytes() int64 {
	n := int64(cap(a.ids))*4 + int64(cap(a.names))*int64(unsafe.Sizeof(""))
	for _, name := range a.names {
		n += int64(len(name))
	}
	return n
}
