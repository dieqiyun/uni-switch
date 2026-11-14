package library

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// uni-switch syncs the library into the agents at every start, and the
// Library page's reads and changes wait on the same lock. Held for the
// whole sync, through every agent's skills, the first operations after
// opening uni-switch took seconds on Windows (#1624, Hipple). The sync lets go
// after each agent's part: a read that comes meanwhile waits for one
// part, and the sync goes on after it.
func TestReadDuringSyncWaitsForOnePart(t *testing.T) {
	sandbox(t)
	src := filepath.Join(t.TempDir(), "src")
	var names []string
	for i := range 4 {
		names = append(names, fmt.Sprintf("skill-%02d", i))
		write(t, filepath.Join(src, names[i], "SKILL.md"), "---\nname: "+names[i]+"\ndescription: a fixture\n---\nbody\n")
	}
	var ids, dirs []string
	for _, tg := range Targets() {
		ids = append(ids, tg.Agent.ID)
		if tg.Skills != "" && !slices.Contains(dirs, tg.Skills) {
			dirs = append(dirs, tg.Skills)
		}
	}
	if len(dirs) < 4 {
		t.Fatalf("skill folders %v, want several agents'", dirs)
	}
	ok(t)(InstallSkills(src, names, ids))
	// what the sync at start has to do: give the skills back to agents
	// that lost them (one installed since, an edit by hand)
	given := func() (n int) {
		for _, d := range dirs {
			if _, err := os.Stat(filepath.Join(d, "skill-00")); err == nil {
				n++
			}
		}
		return n
	}
	had := given()
	if had < 4 {
		t.Fatalf("the install gave %d agents' folders the skills, want several", had)
	}
	for _, d := range dirs {
		for _, n := range names {
			if err := os.RemoveAll(filepath.Join(d, n)); err != nil {
				t.Fatal(err)
			}
		}
	}

	// the sync's looks are slowed, for its parts to take a while; the
	// read's (a read of the page holds the memo) are counted
	look := lookAt
	t.Cleanup(func() { lookAt = look })
	started := make(chan struct{})
	var once, seen sync.Once
	var looks atomic.Int64
	atRead := -1
	lookAt = func(p string) (part, bool) {
		if reading.Load() == nil {
			once.Do(func() { close(started) })
			time.Sleep(100 * time.Microsecond)
			looks.Add(1)
		} else {
			// under the lock, which the sync waits for
			seen.Do(func() { atRead = given() })
		}
		return look(p)
	}
	done := make(chan error, 1)
	go func() {
		_, err := Sync()
		done <- err
	}()
	<-started
	if _, err := Read(nil); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if n := given(); n != had {
		t.Fatalf("the sync gave the skills back to %d of %d folders", n, had)
	}
	t.Logf("the sync looked %d times; the read came with %d of %d folders given back", looks.Load(), atRead, had)
	if atRead < 0 {
		t.Fatal("the read looked at nothing")
	}
	if atRead == had {
		t.Errorf("the read went after the whole sync, every agent's skills given back")
	}
}
