package gateway

import (
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The pauses a group takes add up to its patience, whatever it is set to,
// and not past it: the rounds are a backstop, not a second, shorter limit
// (#1418: patience=240 gave up after 90s of pauses, replan=6). Time is
// counted, not slept: each round's budget is the patience less the pauses
// before it, as waitFrom.Add(patience) is in the candidate loop.
func TestPatienceIsUsedUp(t *testing.T) {
	k := retryPause
	retryPause = time.Second
	t.Cleanup(func() { retryPause = k })
	for _, secs := range []int{30, provider.PatienceDefault, 90, 120, 180, provider.PatienceLongest} {
		patience := time.Duration(secs) * time.Second
		var waited time.Duration
		rounds := 0
		for {
			list := []busyTry{{c: candidate{p: provider.Provider{ID: "a"}, model: "m"}, ready: time.Now()}}
			_, wait, ok := replan(list, rounds, time.Now().Add(-waited), patience)
			if !ok {
				break
			}
			waited += wait
			rounds++
		}
		// the pause that would end past the budget isn't taken: at most the
		// longest pause is left over
		if waited > patience || waited < patience-replanLongest() {
			t.Errorf("patience %s: %d rounds paused %s", patience, rounds, waited)
		}
	}
}

// The backstop's own schedule outlasts the longest patience a group may
// set, with no time spent on the tries between.
func TestReplanRoundsOutlastPatience(t *testing.T) {
	k := retryPause
	retryPause = time.Second
	t.Cleanup(func() { retryPause = k })
	var all time.Duration
	for round := range replanRounds {
		all += min(retryPause<<(round+1), replanLongest())
	}
	if all < provider.PatienceLongest*time.Second {
		t.Fatalf("%d rounds pause %s in all, under PatienceLongest", replanRounds, all)
	}
}
