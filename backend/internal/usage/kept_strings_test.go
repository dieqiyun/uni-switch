package usage

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// An unpacked block's strings share one allocation, so a cache that keeps a
// session id or a model name read from a block must copy it out, or it keeps
// every string of that block: over a long history, the decoded text of all
// of it. This test fills each block with 1 KB of unique error text and gives
// each block its own session, then measures what the Sessions and Routing
// caches hold once the log index itself is let go.
func TestSessionCachesHoldNoDecodedBlocks(t *testing.T) {
	resetUsageTest(t)
	const blocks, text = 16, 1024
	path := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := bufio.NewWriter(f)
	at := time.Now().Add(-time.Hour)
	noise := make([]byte, text/2)
	for b := range blocks {
		for range logBlockRows {
			rand.Read(noise)
			line, _ := json.Marshal(Record{Time: at, Agent: "codex", Session: "s" + string(rune('a'+b)), Provider: "p", Model: "m", Effort: "high", Input: 10, Output: 2, Status: 200, Error: hex.EncodeToString(noise)})
			w.Write(append(line, '\n'))
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	held := func() uint64 {
		var m runtime.MemStats
		runtime.GC()
		runtime.GC()
		runtime.ReadMemStats(&m)
		return m.HeapAlloc
	}
	if got := GatewaySessions(time.Time{}, nil); len(got) != blocks {
		t.Fatalf("sessions: %d, want %d", len(got), blocks)
	}
	if got := Vias(time.Time{}); got.Len() != blocks {
		t.Fatalf("vias: %d, want %d", got.Len(), blocks)
	}
	// the index is kept by design; only what the caches hold is measured
	logIndex.Lock()
	logIndex.snapshot = nil
	logIndex.Unlock()
	withCaches := held()
	gatewayCache.Lock()
	gatewayCache.entry = nil
	gatewayCache.Unlock()
	viasCache.Lock()
	viasCache.all, viasCache.window.m = nil, nil
	viasCache.Unlock()
	without := held()
	decoded := uint64(blocks * logBlockRows * text)
	if withCaches > without && withCaches-without > decoded/8 {
		t.Fatalf("the caches hold %d KB for %d sessions: the decoded text of their blocks (%d KB)", (withCaches-without)>>10, blocks, decoded>>10)
	}
}
