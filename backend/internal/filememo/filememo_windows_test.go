package filememo

import (
	"bytes"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/steady"
)

// Another uni-switch replacing the file (a write is a temp file renamed over
// it) has Windows say, for a moment, that the file isn't there at all: read
// so, a file of accounts was no accounts, and the next write left them out
// (Packing1 on Discord, Windows 11: Antigravity's provider and its account
// gone after an update). Read waits that moment out.
func TestReadWhileReplaced(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "logins.json")
	body := bytes.Repeat([]byte("x"), 100_000)
	os.WriteFile(p, body, 0o600)
	var stop atomic.Bool
	done := make(chan struct{})
	for range 2 {
		go func() {
			defer func() { done <- struct{}{} }()
			for !stop.Load() {
				tmp, err := os.CreateTemp(dir, ".logins.json.*")
				if err != nil {
					continue
				}
				tmp.Write(body)
				tmp.Close()
				if steady.Rename(tmp.Name(), p) != nil {
					os.Remove(tmp.Name())
				}
			}
		}()
	}
	defer func() {
		stop.Store(true)
		<-done
		<-done
	}()
	parse := func(b []byte) (int, error) { return len(b), nil }
	misses := 0
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); {
		if n, err := Read("replaced", p, parse); err != nil || n != len(body) {
			misses++
			if misses < 4 {
				t.Logf("read %d bytes, %v", n, err)
			}
		}
	}
	if misses > 0 {
		t.Fatalf("the file read as not there %d times while replaced", misses)
	}
}
