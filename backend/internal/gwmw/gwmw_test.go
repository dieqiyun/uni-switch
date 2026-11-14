package gwmw

import (
	"slices"
	"sync/atomic"
	"testing"
)

type agent string

func (agent) Begin(Info) Run { return nil }

// Agent middlewares run in the order they were registered; one removed is
// gone, and removing it again takes nothing else.
func TestAgentsInOrder(t *testing.T) {
	ra := Use(agent("a"))
	rb := Use(agent("b"))
	defer rb()
	if got := Agents(); !slices.Equal(got, []Agent{agent("a"), agent("b")}) {
		t.Fatalf("agents %v", got)
	}
	ra()
	ra()
	if got := Agents(); !slices.Equal(got, []Agent{agent("b")}) {
		t.Fatalf("after a's removal %v", got)
	}
}

// With no agent middleware, a refresh is asked only when want says some is
// wanted, at most once a second.
func TestAgentsWantedRefreshesWhenWanted(t *testing.T) {
	ForgetRefreshForTest()
	var asked atomic.Int32
	var remove func()
	stop := Refresh(func() { asked.Add(1); remove = Use(agent("r")) })
	defer stop()
	if a := AgentsWanted(func() bool { return false }); len(a) != 0 || asked.Load() != 0 {
		t.Fatalf("not wanted: %v asked %d", a, asked.Load())
	}
	ForgetRefreshForTest()
	if a := AgentsWanted(func() bool { return true }); len(a) != 1 || asked.Load() != 1 {
		t.Fatalf("wanted: %v asked %d", a, asked.Load())
	}
	remove()
	if a := AgentsWanted(func() bool { return true }); len(a) != 0 || asked.Load() != 1 {
		t.Fatalf("within a second: %v asked %d", a, asked.Load())
	}
}
