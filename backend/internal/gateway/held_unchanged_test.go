package gateway

import (
	"context"
	"fmt"
	"hash/maphash"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// heldWritten lists the held bodies found written in place when they were
// released, for TestMain to fail the run on, and the guard test to find
// its own in.
var heldWritten struct {
	sync.Mutex
	bodies []string
}

// guardHeldBodies has every body holdBody lists checked, at its release,
// against the bytes it was held with (checkHeldBody).
func guardHeldBodies() {
	seed := maphash.MakeSeed()
	checkHeldBody = func(body []byte) func() {
		sum := maphash.Bytes(seed, body)
		return func() {
			if maphash.Bytes(seed, body) == sum {
				return
			}
			heldWritten.Lock()
			heldWritten.bodies = append(heldWritten.bodies, string(body))
			heldWritten.Unlock()
		}
	}
}

// heldWrittenAfter fails a run in which a held body was written in place.
func heldWrittenAfter(code int) int {
	heldWritten.Lock()
	defer heldWritten.Unlock()
	if len(heldWritten.bodies) == 0 {
		return code
	}
	for _, b := range heldWritten.bodies {
		fmt.Fprintf(os.Stderr, "a request body was written in place while held (holdBody): %.200q\n", b)
	}
	return 1
}

// The guard sees a held body written in place, as any route's would be:
// the bodies decodeJSON shares, and factsOf reads once, must stay as they
// were read.
func TestHeldBodyWrittenIsCaught(t *testing.T) {
	if checkHeldBody == nil {
		t.Fatal("TestMain left held bodies unchecked")
	}
	ctx, done := context.WithCancel(context.Background())
	body := []byte(`{"model":"` + t.Name() + `","messages":[{"role":"user","content":"` + strings.Repeat("a", validMemoMin) + `"}]}`)
	holdBody(ctx, body)
	body[len(body)-10] = 'b'
	done()
	marked := string(body)
	for deadline := time.Now().Add(5 * time.Second); ; {
		heldWritten.Lock()
		i := -1
		for j, b := range heldWritten.bodies {
			if b == marked {
				i = j
			}
		}
		if i >= 0 {
			heldWritten.bodies = append(heldWritten.bodies[:i], heldWritten.bodies[i+1:]...)
		}
		heldWritten.Unlock()
		if i >= 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a held body written in place was not caught at its release")
		}
		time.Sleep(time.Millisecond)
	}

	// a body left as it was is not reported
	ctx, done = context.WithCancel(context.Background())
	kept := []byte(`{"model":"` + t.Name() + `-kept","messages":[{"role":"user","content":"` + strings.Repeat("a", validMemoMin) + `"}]}`)
	holdBody(ctx, kept)
	done()
	waitHeldReleased(t, kept)
	heldWritten.Lock()
	defer heldWritten.Unlock()
	for _, b := range heldWritten.bodies {
		if b == string(kept) {
			t.Fatal("a body left unchanged was reported written")
		}
	}
}

// waitHeldReleased waits until body is no longer listed: its release, and
// the check before it, have run.
func waitHeldReleased(t *testing.T, body []byte) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; {
		if !sharesBodyListed(body) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("a body is still held after its request ended")
		}
		time.Sleep(time.Millisecond)
	}
}

func sharesBodyListed(body []byte) bool {
	l := heldBodies.list.Load()
	if l == nil {
		return false
	}
	for _, b := range *l {
		if &b[0] == &body[0] {
			return true
		}
	}
	return false
}
