package filememo

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// A file written just now is read again on each read and compared with
// the bytes parsed, into a buffer kept for it: a new one each time was
// most of what uni-switch allocated as it started, the 1.5 MB config.toml
// Codex's sync had just written read 150 times over (226 MB). A write of
// the same size and time is still seen.
func TestRecentFileReadAgainIntoOneBuffer(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	big := bytes.Repeat([]byte("model = \"gpt-5.5\"\n"), 1<<16)
	if err := os.WriteFile(p, big, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(p, now, now); err != nil {
		t.Fatal(err)
	}
	parses := 0
	parse := func(b []byte) (int, error) { parses++; return len(b), nil }
	if n, err := Read("reread", p, parse); err != nil || n != len(big) {
		t.Fatal(n, err)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	const reads = 20
	for range reads {
		if n, err := Read("reread", p, parse); err != nil || n != len(big) {
			t.Fatal(n, err)
		}
	}
	runtime.ReadMemStats(&after)
	if per := (after.TotalAlloc - before.TotalAlloc) / reads; per*4 > uint64(len(big)) {
		t.Fatalf("a read of a %d-byte file written just now took %d bytes", len(big), per)
	}
	if parses != 1 {
		t.Fatalf("the same bytes parsed %d times", parses)
	}
	if e := seen[memoKey{"reread", p}]; e.raw == nil {
		t.Fatal("the file written just now is not compared byte by byte")
	}
	// reads at once each read into a buffer of their own
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 10 {
				if n, err := Read("reread", p, parse); err != nil || n != len(big) {
					t.Error(n, err)
				}
			}
		})
	}
	wg.Wait()

	// another write of the same size, stamped the same time
	other := bytes.ToUpper(big)
	if err := os.WriteFile(p, other, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, now, now); err != nil {
		t.Fatal(err)
	}
	parse2 := func(b []byte) (int, error) { parses++; return int(b[0]), nil }
	if v, err := Read("reread", p, parse2); err != nil || v != 'M' {
		t.Fatalf("a write of the same size and time read as %v %v", v, err)
	}
	// what was parsed is its own, not the buffer read into again
	if e := seen[memoKey{"reread", p}]; !bytes.Equal(e.raw, other) {
		t.Fatal("the bytes kept are not the file's")
	}
	if err := os.WriteFile(p, big, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, now, now); err != nil {
		t.Fatal(err)
	}
	Read("reread", p, parse2)
	if e := seen[memoKey{"reread", p}]; !bytes.Equal(e.raw, big) {
		t.Fatal("the bytes kept were written over by the next read")
	}
}
