package filememo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// What another process writes to a followed file wakes the loops waiting
// on Written at once, written whole (a new file put in its place), in
// place, or made where there was none; and while that holds the loops
// look for it themselves only now and then (#3185).
func TestWatchWakesOnOthersWrites(t *testing.T) {
	dir := t.TempDir()
	whole, inPlace, made := filepath.Join(dir, "settings.json"), filepath.Join(dir, "providers.json"), filepath.Join(dir, "plugins.yml")
	os.WriteFile(whole, []byte("{}"), 0o600)
	os.WriteFile(inPlace, []byte("{}"), 0o600)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { Watch(ctx, whole, inPlace, made); close(done) }()
	defer func() { cancel(); <-done }()
	for deadline := time.Now().Add(10 * time.Second); OthersEvery() != othersWatched; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the watch never started")
		}
	}
	woken := func(name string, write func()) {
		t.Helper()
		w := Written()
		write()
		select {
		case <-w:
		case <-time.After(10 * time.Second):
			t.Fatalf("%s written by another process woke nothing", name)
		}
	}
	woken("a file written whole", func() {
		tmp := whole + ".tmp"
		os.WriteFile(tmp, []byte(`{"a":1}`), 0o600)
		os.Rename(tmp, whole)
	})
	woken("a file written in place", func() {
		f, _ := os.OpenFile(inPlace, os.O_WRONLY|os.O_APPEND, 0)
		f.WriteString(" ")
		f.Close()
	})
	woken("a file made", func() { os.WriteFile(made, []byte("on: []"), 0o600) })
	cancel()
	<-done
	if OthersEvery() != othersPolled {
		t.Fatalf("with the watch ended the loops look every %v, want %v", OthersEvery(), othersPolled)
	}
}
