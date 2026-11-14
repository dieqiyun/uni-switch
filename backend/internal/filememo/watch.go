package filememo

import (
	"context"
	"log"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
)

// OthersEvery is how often a loop that follows uni-switch's files (Written)
// looks for what another process (the CLI, a second uni-switch) wrote: every
// few seconds, or, while Watch is told of their changes, only now and then
// for one it missed (#3185: the looks woke an idle uni-switch on each beat,
// a dozen times every five seconds).
func OthersEvery() time.Duration {
	if watching.Load() {
		return othersWatched
	}
	return othersPolled
}

const (
	othersPolled  = 5 * time.Second
	othersWatched = time.Minute
)

// watching is whether a Watch is told of the files' changes.
var watching atomic.Bool

// Watch has a change another process makes to one of paths seen at once,
// as a write of uni-switch's own is (Written), until ctx ends. Their folders
// are watched, not the files: a file written whole is a new one put in
// its place. Where the folders can't be watched, the loops go on looking
// every few seconds.
func Watch(ctx context.Context, paths ...string) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("filememo: can't watch for changes: %v", err)
		return
	}
	defer w.Close()
	ours := map[string]bool{}
	dirs := map[string]bool{}
	for _, p := range paths {
		p = filepath.Clean(p)
		ours[p] = true
		dirs[filepath.Dir(p)] = true
	}
	for d := range dirs {
		if err := w.Add(d); err != nil {
			log.Printf("filememo: can't watch %s for changes: %v", d, err)
			return
		}
	}
	watching.Store(true)
	defer watching.Store(false)
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-w.Events:
			if !ok {
				return
			}
			if p := filepath.Clean(e.Name); ours[p] {
				Look(p)
				wake()
			}
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			// a change may be lost (the queue overflowed): looked for at
			// once, and every few seconds again from here
			log.Printf("filememo: watching for changes: %v", err)
			wake()
			return
		}
	}
}
