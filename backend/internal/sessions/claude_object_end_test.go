package sessions

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestObjectEnd(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int
	}{
		{`{}`, 2},
		{`{"a":1},{"b":2}`, 7},
		{`{"a":{"b":[1,{"c":2}]}}tail`, 23},
		{`{"a":"}{]["}x`, 12},
		{`{"a":"\"}"}x`, 11},
		{`{"a":"\\"}x`, 10},
		{`{"a":1`, -1},
		{`{"a":"}`, -1},
		{` {}`, -1},
		{``, -1},
	} {
		if got := objectEnd([]byte(c.in)); got != c.want {
			t.Errorf("objectEnd(%s) = %d, want %d", c.in, got, c.want)
		}
	}
}

// A reply's tool blocks and usage are decoded from the object itself. A
// json.Decoder over the rest of the line copied all of it each time: a
// reply with a large tool input cost its whole length again per block.
func TestClaudeReplyDecodesWithoutCopyingTheLine(t *testing.T) {
	at := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	big := strings.Repeat("x", 1<<20)
	content := `[{"type":"tool_use","id":"tool-1","name":"Write","input":{"content":"` + big + `"}},{"type":"tool_use","id":"tool-2","name":"Skill","input":{"skill":"review"}}]`
	line := claudeUsageLine(at, "a", "req-a", "m", Tokens{Input: 10, Output: 2}, content, true)
	s := &state{}
	claudeLine(s, line, true)
	if s.Models.of("m") != (Tokens{Input: 10, Output: 2}) {
		t.Fatalf("tokens %+v", s.Models.of("m"))
	}
	const n = 20
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range n {
		claudeLine(&state{}, line, true)
	}
	runtime.ReadMemStats(&after)
	// the old decoders copied ~1 MiB a block: 2+ MiB a line
	if per := (after.TotalAlloc - before.TotalAlloc) / n; per > 256<<10 {
		t.Fatalf("a %d KB reply allocated %d KB to read", len(line)>>10, per>>10)
	}
}
