package sessions

import (
	"strings"
	"testing"
	"time"
)

// A line's time is its own "timestamp", the last: a tool's result
// before it may hold another, as Claude Code writes its own after the
// message.
func TestTsAtLastIsTheLines(t *testing.T) {
	inner := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"{\"x\":1,\"y\":{\"timestamp\":\"x\"}} {"}]},"toolUseResult":{"timestamp":"2020-01-01T00:00:00Z"},`
	for _, pad := range []int{0, 2000} {
		b := []byte(inner + `"pad":"` + strings.Repeat("a", pad) + `","timestamp":"2026-10-31T08:00:00.123Z"}`)
		want := time.Date(2026, 10, 31, 8, 0, 0, 123e6, time.UTC)
		if got := tsAt(b, true); !got.Equal(want) {
			t.Fatalf("pad %d: last = %v, want %v", pad, got, want)
		}
		if got := tsAt(b, false); !got.Equal(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("pad %d: first = %v", pad, got)
		}
	}
}
