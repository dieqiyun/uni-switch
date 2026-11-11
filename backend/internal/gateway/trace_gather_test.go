package gateway

import (
	"context"
	"slices"
	"testing"
	"time"
)

// A request's way is told to the trace in a burst of changes (begun, its
// prompt read, a try sent, answered), and each one woke the Routing page's
// long poll on its own: with agents busy, the page read the trace 4-8
// times a second. A waiting read gathers the burst and answers once. It
// also names every route the trace keeps (IDs), so a page that stopped
// reading while out of sight drops one it last heard of under way that has
// ended and gone since.
func TestTraceWaitHearsABurstOnce(t *testing.T) {
	defer func(d time.Duration) { traceGather = d }(traceGather)
	traceGather = 2 * time.Second // the burst's spacing, however loaded the machine, is well inside it
	var s Server
	r := s.trace.begin(Route{Agent: "codex", Model: "m", Tries: []Try{{ID: "relay", Model: "m", Start: time.Now()}}})
	after := s.Trace(context.Background(), 0, 0).Seq

	reads := 0
	heard := make(chan TraceState, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		for at := after; ; {
			st := s.Trace(ctx, at, 10*time.Second)
			reads++
			at = st.Seq
			if i := slices.IndexFunc(st.Routes, func(x Route) bool { return x.ID == r.ID && x.Done }); i >= 0 || ctx.Err() != nil {
				heard <- st
				return
			}
		}
	}()
	// the read is waiting before the burst begins
	for waiting := false; !waiting; {
		s.trace.mu.Lock()
		waiting = s.trace.wake != nil
		s.trace.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	steps := []func(*Route){
		func(x *Route) { x.Prompt = &Prompt{} },
		func(x *Route) { x.Tries[0].Status = 200 },
		func(x *Route) { x.Tries[0].Done, x.Tries[0].Millis = true, 900 },
		func(x *Route) { x.Done, x.Status, x.Millis = true, 200, 900 },
	}
	for _, f := range steps {
		s.trace.update(r, f)
		time.Sleep(20 * time.Millisecond)
	}
	st := <-heard
	if reads != 1 {
		t.Fatalf("a burst of %d changes was read in %d answers, want 1", len(steps), reads)
	}
	if len(st.Routes) != 1 || st.Routes[0].Prompt == nil || !st.Routes[0].Tries[0].Done {
		t.Fatalf("the answer has %+v, want the route with the whole burst", st.Routes)
	}
	if !slices.Equal(st.IDs, []int64{r.ID}) {
		t.Fatalf("ids %v, want [%d]", st.IDs, r.ID)
	}
}
