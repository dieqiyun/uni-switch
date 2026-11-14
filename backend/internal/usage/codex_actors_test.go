package usage

import (
	"fmt"
	"testing"
	"time"
)

// A day's rows are one thread's turns after another: each keeps its
// thread in 4 bytes, not a string's 16, and reads it back (#2278).
func TestCodexActorsTakeFourBytesARow(t *testing.T) {
	const rows, threads = 100_000, 200
	c := &rowChunk{}
	at := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	actor := func(i int) string {
		if i%7 == 0 {
			return "" // a row of no Codex thread
		}
		return fmt.Sprintf("019a%028d", i*threads/rows)
	}
	for i := range rows {
		c.add(Row{Record: Record{Time: at, Agent: "codex"}, codexActor: actor(i)}, "", int64(i), false)
	}
	for i := range rows {
		if got := c.codexActors.at(i); got != actor(i) {
			t.Fatalf("row %d: actor %q, want %q", i, got, actor(i))
		}
	}
	if got, most := c.codexActors.bytes(), int64(rows*4*5/4+threads*64); got > most {
		t.Fatalf("%d rows of %d threads hold %d bytes of actors, want at most %d", rows, threads, got, most)
	}
	clone := c.codexActors.clone()
	setCodexActor(c, 3, "another")
	if clone.at(3) != actor(3) || c.codexActors.at(3) != "another" || clone.equal(c.codexActors) {
		t.Fatalf("a clone saw its original's change: %q, %q", clone.at(3), c.codexActors.at(3))
	}
	if len(clone.names) != threads+1 {
		t.Fatalf("clone names = %d, want %d", len(clone.names), threads+1)
	}
}
