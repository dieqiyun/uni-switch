package gateway

import (
	"runtime"
	"strings"
	"testing"
)

// collectStream is a streamed reply as the collector takes it: n deltas
// of thinking, then n of text, then a tool call and more text.
func collectStream(c *collector, n int) {
	c.add(Event{Kind: KStart, MsgID: "m", Model: "x"})
	for i := range n {
		c.add(Event{Kind: KThink, Text: "think " + string(rune('a'+i%26)) + " "})
	}
	c.add(Event{Kind: KSig, Text: "sig"})
	for i := range n {
		c.add(Event{Kind: KText, Text: "word " + string(rune('a'+i%26)) + " of the reply, "})
	}
	c.add(Event{Kind: KToolStart, ID: "t1", Name: "Read"})
	c.add(Event{Kind: KToolArgs, Text: `{"path":"/a"}`})
	c.add(Event{Kind: KText, Text: "after "})
	c.add(Event{Kind: KText, Text: "the call"})
	c.add(Event{Kind: KStop, Stop: "stop"})
}

// The collector's reply is the stream's, delta for delta: each part's
// text whole, in its own part, a part begun after a call its own.
func TestCollectorKeepsEveryDelta(t *testing.T) {
	var c collector
	collectStream(&c, 500)
	res := c.finish()
	var think, text strings.Builder
	for i := range 500 {
		think.WriteString("think " + string(rune('a'+i%26)) + " ")
		text.WriteString("word " + string(rune('a'+i%26)) + " of the reply, ")
	}
	if len(res.Parts) != 4 {
		t.Fatalf("parts %+v", res.Parts)
	}
	if p := res.Parts[0]; p.Kind != Thinking || p.Text != think.String() || p.Signature != "sig" {
		t.Errorf("thinking %q sig %q", p.Text[:min(40, len(p.Text))], p.Signature)
	}
	if p := res.Parts[1]; p.Kind != Text || p.Text != text.String() {
		t.Errorf("text %d bytes, want %d", len(p.Text), text.Len())
	}
	if p := res.Parts[2]; p.Kind != ToolCall || string(p.Args) != `{"path":"/a"}` {
		t.Errorf("call %+v", p)
	}
	if p := res.Parts[3]; p.Kind != Text || p.Text != "after the call" {
		t.Errorf("last text %q", p.Text)
	}
}

// A streamed reply's text is gathered in time linear in its length: the
// collector used to add each delta to the part's string, copying the whole
// reply so far every time, about 57 KB a delta on a 4000-delta reply (a
// Chat upstream to an Anthropic client copied 229 MB to stream 88 KB).
func TestCollectorTextLinear(t *testing.T) {
	const n = 4000
	var ms runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&ms)
	before := ms.TotalAlloc
	var c collector
	collectStream(&c, n)
	res := c.finish()
	runtime.ReadMemStats(&ms)
	got := ms.TotalAlloc - before
	size := 0
	for _, p := range res.Parts {
		size += len(p.Text)
	}
	// the strings themselves, the builder's doublings, the parts: a few
	// times the reply. The quadratic copy was ~150× it here.
	if got > uint64(16*size) {
		t.Fatalf("collecting a %d-byte reply allocated %d bytes (%.0f×)", size, got, float64(got)/float64(size))
	}
}

func BenchmarkCollectorText(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		var c collector
		collectStream(&c, 4000)
		c.finish()
	}
}
