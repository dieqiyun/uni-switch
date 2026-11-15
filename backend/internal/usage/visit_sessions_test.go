package usage

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

// sessionChunks are benchChunks with a row in every tenth told its
// agent's own conversation id, "conv-<chunk>".
func sessionChunks(t testing.TB, n, size int) []*rowChunk {
	t.Helper()
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	var chunks []*rowChunk
	for k := range n {
		c := &rowChunk{}
		for i := range size {
			at = at.Add(time.Second)
			r := Record{Time: at, Agent: "codex", Provider: fmt.Sprint("p", k%3), Model: "gpt-5", Input: i, Session: fmt.Sprint("s", k), RequestID: fmt.Sprint("req-", k, "-", i)}
			if i%10 == 0 {
				r.NativeSession = fmt.Sprint("conv-", k)
			}
			c.add(Row{Record: r}, fmt.Sprint("resp-", k, "-", i), int64(i), false)
		}
		packed := c.pack()
		if len(packed.Archive) == 0 {
			t.Fatal("not packed")
		}
		chunks = append(chunks, packed)
	}
	return chunks
}

// visitSessions hands on the records visit does whose Session or
// NativeSession is named, from since on, and nothing else.
func TestVisitSessionsIsVisitOfThoseSessions(t *testing.T) {
	s := &logSnapshot{blocks: sessionChunks(t, 12, 200)}
	since := s.blocks[3].Latest.Add(-50 * time.Second)
	// twice: the second pass reads only the blocks it learnt name them
	cases := [][]string{{"s5"}, {"conv-7"}, {"s4", "conv-9", "nobody"}, {"s3"}, {"nobody"}, {""}}
	for _, named := range append(cases, cases...) {
		var want, got []Record
		s.visit(since, func(r Record) {
			if r.Session != "" && slices.Contains(named, r.Session) || r.NativeSession != "" && slices.Contains(named, r.NativeSession) {
				want = append(want, r)
			}
		})
		s.visitSessions(since, named, func(r Record) { got = append(got, r) })
		if len(got) != len(want) {
			t.Fatalf("%q: %d records, want %d", named, len(got), len(want))
		}
		for i := range want {
			if got[i].RequestID != want[i].RequestID || !got[i].Time.Equal(want[i].Time) || got[i].Provider != want[i].Provider {
				t.Fatalf("%q record %d: %+v, want %+v", named, i, got[i], want[i])
			}
		}
	}
}

// A record visitSessions hands on keeps its strings after the next
// block is decoded over the one it came from.
func TestVisitSessionsRecordsKeepTheirStrings(t *testing.T) {
	s := &logSnapshot{blocks: sessionChunks(t, 6, 300)}
	var got []Record
	s.visitSessions(time.Time{}, []string{"conv-0", "conv-1", "conv-2", "conv-3", "conv-4", "conv-5"}, func(r Record) { got = append(got, r) })
	if len(got) != 6*30 {
		t.Fatalf("%d records, want %d", len(got), 6*30)
	}
	for i, r := range got {
		k, n := i/30, i%30*10
		if r.RequestID != fmt.Sprint("req-", k, "-", n) || r.NativeSession != fmt.Sprint("conv-", k) || r.Session != fmt.Sprint("s", k) {
			t.Fatalf("record %d reads %q %q %q after the blocks after it were decoded", i, r.RequestID, r.NativeSession, r.Session)
		}
	}
}

// BenchmarkLeadLookup is Codex's lead lookup over a month's log: every
// record, then the ones of one conversation, against those alone (#4370).
func BenchmarkLeadLookup(b *testing.B) {
	s := &logSnapshot{blocks: sessionChunks(b, 40, 3000)}
	conv := []string{"conv-17"}
	b.Run("visit", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			n := 0
			s.visit(time.Time{}, func(r Record) {
				if slices.Contains(conv, r.NativeSession) || slices.Contains(conv, r.Session) {
					n++
				}
			})
			if n != 300 {
				b.Fatal(n)
			}
		}
	})
	b.Run("visitSessions", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			n := 0
			s.visitSessions(time.Time{}, conv, func(Record) { n++ })
			if n != 300 {
				b.Fatal(n)
			}
		}
	})
}

// A block that names none of the sessions looked up is not decoded once
// its sessions are known.
func TestVisitSessionsSkipsBlocksOfOtherSessions(t *testing.T) {
	s := &logSnapshot{blocks: sessionChunks(t, 20, 2000)}
	decoded := 0
	sessionsDecoding = func() { decoded++ }
	t.Cleanup(func() { sessionsDecoding = nil })
	s.visitSessions(time.Time{}, []string{"nobody"}, func(Record) {})
	if decoded != 20 {
		t.Fatalf("the first lookup decoded %d blocks of 20", decoded)
	}
	decoded = 0
	n := 0
	s.visitSessions(time.Time{}, []string{"conv-19", "nobody"}, func(Record) { n++ })
	if decoded != 1 || n != 200 {
		t.Fatalf("a lookup of one block's conversation decoded %d blocks of 20 and read %d records, want 1 and 200", decoded, n)
	}
}
