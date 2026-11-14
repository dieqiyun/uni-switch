package usage

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The usage page's cells and the heatmap's days keep unique's handles and
// numbers, never a row's strings, so they read a chunk's strings where it
// was decoded: a copy of each chunk's strings was ~56MB a poll on a real
// history, every chunk read again for the 7- and 30-day pages (#2278).
func TestPartsReadStringsWhereDecoded(t *testing.T) {
	now := time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)
	var chunks []*rowChunk
	section := 0
	for k := range 8 {
		c := &rowChunk{}
		for i := range 2000 {
			id := fmt.Sprintf("req-%d-%d-%s", k, i, strings.Repeat("x", 60))
			section += len(id)
			c.add(Row{Record: Record{Time: now.Add(-time.Duration(k*2000+i) * time.Second), Agent: "claude", Provider: "p", Model: fmt.Sprint("model-", k), Session: "s", Input: 1, Status: 200, RequestID: id}}, "", int64(i), false)
		}
		packed := c.pack()
		if len(packed.Archive) == 0 {
			t.Fatal("a chunk wasn't packed")
		}
		chunks = append(chunks, packed)
	}
	t.Cleanup(ReleaseScratch)
	ps := pairRows(All, now, chunks, nil, nil, nil)
	pass := newCellPass(All, Filter{}, now, true)
	since := now.AddDate(0, 0, -1)
	allocs := func(name string, fn func()) {
		t.Helper()
		fn() // the decode buffer, kept from here on
		least := ^uint64(0)
		for range 3 {
			var a, b runtime.MemStats
			runtime.ReadMemStats(&a)
			fn()
			runtime.ReadMemStats(&b)
			least = min(least, b.TotalAlloc-a.TotalAlloc)
		}
		if least > uint64(section/4) {
			t.Errorf("%s allocated %d bytes for %d bytes of strings", name, least, section)
		}
	}
	allocs("partsOf", func() { ps.partsOf(pass, nil, nil) })
	allocs("cellsBefore", func() { ps.cellsBefore(pass, now.Add(time.Second)) })
	allocs("heatDaysOf", func() { ps.heatDaysOf(since, now, Filter{}, nil, nil, nil) })

	// what they keep is right after every buffer was read over again
	parts := ps.partsOf(pass, nil, nil)
	before := ps.cellsBefore(pass, now.Add(time.Second))
	days := ps.heatDaysOf(since, now, Filter{}, nil, nil, nil)
	ps.partsOf(newCellPass(All, Filter{Model: "model-3"}, now, true), nil, nil)
	models := map[string]int{}
	for _, p := range parts {
		for _, c := range p.cells {
			models[c.model.Value()] += c.n
		}
	}
	for k := range 8 {
		if n := models[fmt.Sprint("model-", k)]; n != 2000 {
			t.Fatalf("model-%d has %d calls in the parts: %v", k, n, models)
		}
	}
	n := 0
	for _, c := range before.cells {
		if !strings.HasPrefix(c.model.Value(), "model-") || c.provider.Value() != "p" || c.agent.Value() != "claude" {
			t.Fatalf("a cell before the cut reads %q %q %q", c.provider.Value(), c.agent.Value(), c.model.Value())
		}
		n += c.n
	}
	if n != 16000 {
		t.Fatalf("%d calls before the cut", n)
	}
	calls := 0
	for _, d := range days {
		if d.Date != "2026-10-01" && d.Date != "2026-09-30" {
			t.Fatalf("a day reads %q", d.Date)
		}
		calls += d.Calls
	}
	if calls != 16000 {
		t.Fatalf("%d calls in the days", calls)
	}
}
