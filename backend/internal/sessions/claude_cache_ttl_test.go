package sessions

import (
	"fmt"
	"testing"
	"time"
)

// PAMI on Discord: Claude Code's transcript says which cache writes were
// kept for an hour (cache_creation's ephemeral_1h_input_tokens), which
// Anthropic bills at 2× input, not 1.25×. Both readers keep them apart; a
// line without the split reads as before.
func TestClaudeCacheWriteSplit(t *testing.T) {
	for _, fast := range []bool{false, true} {
		t.Run(fmt.Sprint("fast=", fast), func(t *testing.T) {
			s := &state{}
			at := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
			id := `"id":"a","model":"m"`
			if fast {
				id = `"model":"m","id":"a"`
			}
			claudeLine(s, []byte(fmt.Sprintf(`{"type":"assistant","timestamp":%q,"requestId":"req-a","message":{%s,"role":"assistant","content":[],"usage":{"input_tokens":10,"output_tokens":2,"cache_read_input_tokens":30,"cache_creation_input_tokens":3000,"cache_creation":{"ephemeral_5m_input_tokens":1000,"ephemeral_1h_input_tokens":2000}}}}`,
				at.Format(time.RFC3339Nano), id)), true)
			claudeLine(s, claudeUsageLine(at.Add(time.Second), "b", "req-b", "m", Tokens{Input: 1, CacheWrite: 500}, "", fast), true)
			if want := (Tokens{Input: 11, Output: 2, CacheRead: 30, CacheWrite: 3500, CacheWrite1h: 2000}); s.Models.of("m") != want {
				t.Fatalf("tokens = %+v, want %+v", s.Models.of("m"), want)
			}
		})
	}
	if got := ccTokens(1, 2, 3, 0, &ccCacheCreation{Ephemeral1h: 40}); got.CacheWrite != 40 || got.CacheWrite1h != 40 {
		t.Errorf("a split with no total = %+v", got)
	}
}
