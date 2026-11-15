package agent

import (
	"reflect"
	"sync"
	syncatomic "sync/atomic"
	"time"
)

// listRunning lists the processes a pattern matches as they are now, for
// a caller that waits on them (a restart waiting for the app to end).
// What it finds replaces what runningLately kept, so a restart's last
// look, the one that finds the app gone, is what the next poll sees.
func listRunning(pat string) []runningProc {
	ps := listRunningNow(pat)
	lately.put(pat, time.Now(), ps)
	return ps
}

// runningLately is running(pat) as it was up to runningFor ago.
// /api/state asks StaleCopies for every wired agent, and each pattern
// started a pgrep that walks every process's arguments: most of a state
// call's 45ms (9ms without it), on every page switch, save and poll.
// Each process's start is kept, not its age, so an age read later is
// still right.
const runningFor = 3 * time.Second

// listRuns counts the listings runningLately made (a test reads it).
var listRuns syncatomic.Uint64

var lately runningKept

type runningKept struct {
	sync.Mutex
	by map[string]keptRun
}

type keptRun struct {
	at    time.Time
	start []time.Time
	cmd   []string
}

func (k *runningKept) put(pat string, now time.Time, ps []runningProc) {
	r := keptRun{at: now}
	for _, p := range ps {
		r.start = append(r.start, now.Add(-p.up))
		r.cmd = append(r.cmd, p.cmd)
	}
	k.Lock()
	if k.by == nil {
		k.by = map[string]keptRun{}
	}
	k.by[pat] = r
	k.Unlock()
}

func runningLately(pat string) []runningProc {
	// a test's own running is asked each time
	if reflect.ValueOf(running).Pointer() != reflect.ValueOf(listRunning).Pointer() {
		return running(pat)
	}
	now := time.Now()
	lately.Lock()
	r, ok := lately.by[pat]
	lately.Unlock()
	if !ok || now.Sub(r.at) >= runningFor || now.Before(r.at) {
		listRuns.Add(1)
		return listRunning(pat)
	}
	ps := make([]runningProc, len(r.cmd))
	for i := range r.cmd {
		ps[i] = runningProc{now.Sub(r.start[i]), r.cmd[i]}
	}
	return ps
}
