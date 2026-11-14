package sessions

import (
	"sync"
	"time"
)

// listMemo keeps what a listing read from each of many small files, by the
// file's own size and mtime, so a listing that runs on every poll opens only
// the files that changed since the last one rather than every session's.
// Each listing is a round: a file it did not ask about (a session deleted)
// is dropped when the round ends. A read that failed is not kept, so the
// next listing tries again: a failed read means unknown, not empty.
type listMemo[T any] struct {
	sync.Mutex
	m map[string]memoEntry[T]
}

type memoEntry[T any] struct {
	size, mod int64
	v         T
}

type memoRound[T any] struct {
	l         *listMemo[T]
	old, next map[string]memoEntry[T]
}

// round starts a listing.
func (l *listMemo[T]) round() *memoRound[T] {
	l.Lock()
	old := l.m
	l.Unlock()
	return &memoRound[T]{l: l, old: old, next: make(map[string]memoEntry[T], len(old))}
}

// get is what read gives for the file at path, read again only when its
// size or mtime is not what it was when last read.
func (r *memoRound[T]) get(path string, size int64, mod time.Time, read func() (T, bool)) (T, bool) {
	if e, ok := r.old[path]; ok && e.size == size && e.mod == mod.UnixNano() {
		r.next[path] = e
		return e.v, true
	}
	v, ok := read()
	if ok {
		r.next[path] = memoEntry[T]{size, mod.UnixNano(), v}
	}
	return v, ok
}

// done keeps what this round read, and only that, for the next.
func (r *memoRound[T]) done() {
	r.l.Lock()
	r.l.m = r.next
	r.l.Unlock()
}

func (l *listMemo[T]) reset() {
	l.Lock()
	l.m = nil
	l.Unlock()
}
