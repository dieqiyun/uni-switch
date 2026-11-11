package gateway

import (
	"encoding/json"
	"testing"
)

// #589: a Responses upstream's cache_write_tokens is counted as a cache
// write, not as input, as Codex counts it from the same reply; and a reply
// translated for Codex tells it what was written.
func TestResponsesUsageCacheWrite(t *testing.T) {
	for _, c := range []struct {
		name, usage string
		want        Usage
	}{
		{"read", `{"input_tokens":120000,"output_tokens":300,"input_tokens_details":{"cached_tokens":113000}}`, Usage{Input: 7000, Output: 300, CacheRead: 113000}},
		{"read and write", `{"input_tokens":130000,"output_tokens":400,"input_tokens_details":{"cached_tokens":113000,"cache_write_tokens":9000},"output_tokens_details":{"reasoning_tokens":50}}`,
			Usage{Input: 8000, Output: 400, CacheRead: 113000, CacheWrite: 9000, Reasoning: 50}},
		{"none", `{"input_tokens":1000,"output_tokens":5}`, Usage{Input: 1000, Output: 5}},
	} {
		var u rUsage
		if err := json.Unmarshal([]byte(c.usage), &u); err != nil {
			t.Fatal(err)
		}
		got := u.usage()
		if got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
		// told to Codex again, it reads back the same
		b, _ := json.Marshal(got.responses())
		var back rUsage
		if err := json.Unmarshal(b, &back); err != nil || back.usage() != c.want {
			t.Errorf("%s: told as %s, read back %+v", c.name, b, back.usage())
		}
	}
}
