package gateway

import (
	"bytes"
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// A body being served is known by where it is, not by a sum of all its
// bytes on every decode (#4371): its dozen decodes, its topMember reads
// and its validity checks hash none of it, and check and split it once.
// Released, the same bytes are summed again, as any other body's.
func TestHeldBodyIsNotSummed(t *testing.T) {
	body := memoBody(t.Name())
	var sums atomic.Int32
	f := func(data []byte) {
		if bytes.Equal(data, body) {
			sums.Add(1)
		}
	}
	validHashed.Store(&f)
	t.Cleanup(func() { validHashed.Store(nil) })
	scans := countScans(t, body)
	ctx, done := context.WithCancel(context.Background())
	holdBody(ctx, body)
	for range 6 {
		var v struct {
			Model string `json:"model"`
		}
		if err := decodeJSON(body, &v); err != nil || v.Model != t.Name() {
			t.Fatalf("decode: %v %q", err, v.Model)
		}
		if m, found, ok := topMember(body, "model"); !ok || !found || string(m) != `"`+t.Name()+`"` {
			t.Fatalf("topMember: %q %v %v", m, found, ok)
		}
		if !jsonValid(body) {
			t.Fatal("a held body read as not valid")
		}
	}
	if n := sums.Load(); n != 0 {
		t.Fatalf("a held %d-byte body was summed %d times", len(body), n)
	}
	if n := scans.Load(); n != 1 {
		t.Fatalf("a held body was checked %d times, want 1", n)
	}
	done()
	// the release runs on its own goroutine (context.AfterFunc)
	for deadline := time.Now().Add(10 * time.Second); ; {
		heldBodies.Lock()
		_, kept := heldBodies.long[&body[0]]
		heldBodies.Unlock()
		if !kept {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a released body's finds were kept")
		}
		time.Sleep(time.Millisecond)
	}
	if !jsonValid(body) || sums.Load() != 1 {
		t.Fatalf("a released body was not summed again (%d)", sums.Load())
	}
}

// A held body that isn't JSON is said to be not valid while held, as
// before, and decodes to encoding/json's error.
func TestHeldBodyNotJSON(t *testing.T) {
	body := append(memoBody(t.Name()), '}')
	ctx, done := context.WithCancel(context.Background())
	defer done()
	holdBody(ctx, body)
	for range 2 {
		if jsonValid(body) {
			t.Fatal("a held body with a stray } read as valid")
		}
		var v struct{ Model string }
		if err := decodeJSON(body, &v); err == nil {
			t.Fatal("a held body that isn't JSON decoded")
		}
		if _, _, ok := topMember(body, "model"); ok {
			t.Fatal("topMember read a held body that isn't JSON")
		}
	}
}
