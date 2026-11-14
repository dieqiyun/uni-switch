package usage

import (
	"testing"
	"time"
)

// A named chunk's rows before the cut are counted once for a cut, not
// unpacked again for each read of the heatmap, the page and Codex's
// effort (#2278), and sum as counting every chunk does.
func TestHeatBeforeKeptBySerial(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	cut := now.Add(-48 * time.Hour)
	var set []*rowChunk
	for k := range 5 {
		c := &rowChunk{}
		for i := range 40 {
			r := Record{Time: now.Add(-time.Duration(k*20+i) * 2 * time.Hour), Agent: "claude", Provider: "relay", Input: 1 + i%5, Output: k}
			c.add(Row{Record: r}, "", int64(i), false)
		}
		if k < 4 {
			c.serial = nextChunkSerial()
		}
		set = append(set, c)
	}
	want := func(cut time.Time) heatSum {
		var s heatSum
		for _, c := range set {
			for i := range c.Rows {
				if p := &c.Rows[i]; p.Time().Before(cut) {
					s.n++
					s.at += p.Time().UnixNano()
					for _, v := range p.Tokens {
						s.tokens += int64(v)
					}
				}
			}
		}
		return s
	}
	var opened int
	heatBeforeOpening = func(*rowChunk) { opened++ }
	t.Cleanup(func() { heatBeforeOpening = nil })
	for i, step := range []struct {
		cut    time.Time
		opened int
	}{{cut, 5}, {cut, 1}, {cut, 1}, {cut.Add(-24 * time.Hour), 5}, {cut.Add(-24 * time.Hour), 1}} {
		opened = 0
		got := heatBefore(step.cut, [3][]*rowChunk{set[:2], set[2:4], set[4:]})
		if w := want(step.cut); got != w || w.n == 0 {
			t.Fatalf("read %d: %+v, want %+v", i, got, w)
		}
		if opened != step.opened {
			t.Fatalf("read %d unpacked %d chunks, want %d", i, opened, step.opened)
		}
	}
}
