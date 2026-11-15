package sessions

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// oldPrefixHash is prefixHash as it was, through io.CopyN: a stored hash
// must still match after the change, or every cache is read again.
func oldPrefixHash(path string, n int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	const window, samples = int64(4096), int64(16)
	if n <= window*samples {
		if _, err = io.CopyN(h, f, n); err != nil {
			return ""
		}
	} else {
		for i := int64(0); i < samples; i++ {
			off := (n - window) * i / (samples - 1)
			if _, err = io.CopyN(h, io.NewSectionReader(f, off, window), window); err != nil {
				return ""
			}
		}
	}
	return fmt.Sprintf("sample-v1:%x", h.Sum(nil))
}

func TestPrefixHashUnchangedAndUnbuffered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.jsonl")
	b := make([]byte, 3<<20)
	for i := range b {
		b[i] = byte(i*7 + i/4096)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int64{0, 1, 4095, 4096, 65535, 65536, 65537, 1 << 20, 3 << 20} {
		if got, want := prefixHash(path, n), oldPrefixHash(path, n); got != want || got == "" {
			t.Fatalf("n=%d: %q, was %q", n, got, want)
		}
	}
	// Past the file's end is unknown, as it was.
	for _, n := range []int64{3<<20 + 1, 4 << 20} {
		if got := prefixHash(path, n); got != "" {
			t.Fatalf("n=%d past the end: %q", n, got)
		}
		if old := oldPrefixHash(path, n); old != "" {
			t.Fatalf("n=%d: the old hash was %q", n, old)
		}
	}

	prefixHash(path, 1<<20)
	var a, z runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	const runs = 50
	for i := 0; i < runs; i++ {
		prefixHash(path, 1<<20)
	}
	runtime.ReadMemStats(&z)
	// A cold call allocates one 64 KB scratch buffer. Both the collector
	// and the race runtime may drop pooled buffers at any time, so the
	// byte bound must allow every call to be cold. Count allocations too:
	// the old io.CopyN path allocates readers and buffers for all 16
	// samples, unlike a cold call's one scratch buffer.
	if per := (z.TotalAlloc - a.TotalAlloc) / runs; per > 80<<10 {
		t.Fatalf("hashing a long log allocated %d KB", per>>10)
	}
	if per := testing.AllocsPerRun(runs, func() { prefixHash(path, 1<<20) }); per > 16 {
		t.Fatalf("hashing a long log made %.0f allocations", per)
	}
}
