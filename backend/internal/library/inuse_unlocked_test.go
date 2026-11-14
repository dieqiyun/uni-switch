package library

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// #2774: boot.js, which the window waits on before it paints, asks InUse
// through the built-in plugins, and a library sync holds mu for as long as
// it takes (13–16s on the reporter's Mac): the window stayed white that
// long. InUse answers while a sync holds the lock, from the file as it is.
func TestInUseAnswersWhileASyncHoldsTheLock(t *testing.T) {
	sandbox(t)
	os.MkdirAll(filepath.Dir(path()), 0o700)
	write(t, path(), `{"skills":[{"name":"a"},{"name":"b"}],"mcp":[{"name":"s"}]}`)

	mu.Lock() // a sync, still running
	defer mu.Unlock()
	type res struct {
		n   int
		err error
	}
	got := make(chan res, 1)
	go func() {
		n, err := InUse()
		got <- res{n, err}
	}()
	select {
	case r := <-got:
		if r.err != nil || r.n != 3 {
			t.Fatalf("InUse = %d, %v; want 3", r.n, r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("InUse waited on the sync's lock")
	}

	// a file that can't be read is still an error, not 0
	write(t, path(), `{"skills":`)
	if n, err := InUse(); err == nil {
		t.Fatalf("a broken library.json read as %d", n)
	}
}
