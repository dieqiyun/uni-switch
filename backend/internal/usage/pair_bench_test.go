package usage

import (
	"fmt"
	"testing"
	"time"
)

// pairFixture is a long history as pairRows meets it on a poll: files
// session files of calls Codex calls each, every one also through the
// gateway by its response id, and as many Claude Code calls matched by
// session and tokens.
func pairFixture(files, calls int) (gateways, locals []*rowChunk) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	// serials of their own: two fixtures' chunks under one serial read each
	// other's kept message keys (msgKeysOf), whichever test ran first
	g := &rowChunk{serial: nextChunkSerial()}
	n := 0
	for f := 0; f < files; f++ {
		c := &rowChunk{serial: nextChunkSerial()}
		for i := 0; i < calls; i++ {
			at := now.Add(-time.Duration(n) * 20 * time.Second)
			n++
			if f%2 == 0 {
				id := fmt.Sprintf("resp_%d", n)
				g.add(Row{Record: Record{Time: at, Agent: "codex", Provider: "openai", Model: "m", Input: 100 + i, Output: 5, ResponseID: id, Session: fmt.Sprint("s", f)}}, "", int64(n), false)
				c.add(Row{Record: Record{Time: at, Agent: "codex", Provider: "openai", Model: "m", Input: 100 + i, Output: 5, ResponseID: id, Session: fmt.Sprint("s", f)}, Source: "log"}, "", int64(i), false)
			} else {
				g.add(Row{Record: Record{Time: at, Agent: "claude-code", Provider: "anthropic", Model: "c", Input: 100 + i, Output: 7, Session: fmt.Sprint("c", f)}}, "", int64(n), false)
				c.add(Row{Record: Record{Time: at, Agent: "claude-code", Provider: "anthropic", Model: "c", Input: 100 + i, Output: 7, Session: fmt.Sprint("c", f)}, Source: "log"}, fmt.Sprint("msg_", n), int64(i), false)
			}
		}
		locals = append(locals, c)
	}
	return []*rowChunk{g}, locals
}

func BenchmarkPairRows(b *testing.B) {
	gateways, locals := pairFixture(2000, 100)
	now := time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ps := pairRows(All, now, gateways, locals, nil, nil)
		if ps.matched.count() != 200000 {
			b.Fatal(ps.matched.count())
		}
	}
}
