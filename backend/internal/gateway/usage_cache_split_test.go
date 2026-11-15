package gateway

import (
	"encoding/json"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func splitUsage(t *testing.T, raw string) Usage {
	t.Helper()
	var a aUsage
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatal(err)
	}
	return a.usage()
}

func wantUsageSplit(t *testing.T, u Usage, want *aCacheCreation) {
	t.Helper()
	got := u.anthropic().CacheCreation
	if (got == nil) != (want == nil) || got != nil && *got != *want {
		t.Fatalf("cache_creation = %+v, want %+v", got, want)
	}
}

func TestAnthropicCacheSplitPresence(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		want      *aCacheCreation
	}{
		{"pure5m", `{"cache_creation_input_tokens":1894,"cache_creation":{"ephemeral_5m_input_tokens":1894,"ephemeral_1h_input_tokens":0}}`, &aCacheCreation{Ephemeral5m: 1894}},
		{"reportedZero", `{"cache_creation_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0}}`, &aCacheCreation{}},
		{"totalFromSplit", `{"cache_creation":{"ephemeral_5m_input_tokens":40,"ephemeral_1h_input_tokens":0}}`, &aCacheCreation{Ephemeral5m: 40}},
		{"unreported", `{"cache_creation_input_tokens":40}`, nil},
		{"null", `{"cache_creation_input_tokens":40,"cache_creation":null}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := splitUsage(t, tc.raw)
			wantUsageSplit(t, u, tc.want)
			// A later output-only event must not erase an earlier split or
			// manufacture one when the upstream never reported it.
			u.add(splitUsage(t, `{"output_tokens":7}`))
			wantUsageSplit(t, u, tc.want)
		})
	}
}

func TestAnthropicCacheSplitUpdatesAndTotals(t *testing.T) {
	hour := splitUsage(t, `{"cache_creation_input_tokens":20,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":20}}`)
	short := splitUsage(t, `{"cache_creation_input_tokens":30,"cache_creation":{"ephemeral_5m_input_tokens":30,"ephemeral_1h_input_tokens":0}}`)
	zero := splitUsage(t, `{"cache_creation_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0}}`)
	u := hour
	u.add(short)
	wantUsageSplit(t, u, &aCacheCreation{Ephemeral5m: 10, Ephemeral1h: 20})
	u.add(zero)
	wantUsageSplit(t, u, &aCacheCreation{Ephemeral5m: 10, Ephemeral1h: 20})

	for _, only := range []bool{false, true} {
		wantUsageSplit(t, short.plus(hour, only), &aCacheCreation{Ephemeral5m: 30, Ephemeral1h: 20})
	}
	wantUsageSplit(t, zero.plus(hour, false), &aCacheCreation{Ephemeral1h: 20})
	wantUsageSplit(t, zero.plus(hour, true), &aCacheCreation{})
	// Summing a completed reply retains the split. A sparse output update
	// carries no new cache counters, so it must not reset the previous ones.
	wantUsageSplit(t, Usage{}.plus(short, false), &aCacheCreation{Ephemeral5m: 30})
	update := (Usage{Output: 7}).plus(hour, true)
	wantUsageSplit(t, update, nil)
	u = short
	u.add(update)
	wantUsageSplit(t, u, &aCacheCreation{Ephemeral5m: 30})

	// Existing explicit 1h accounting still emits a split even without
	// presence evidence, including its cap to the total writes.
	wantUsageSplit(t, Usage{CacheWrite: 40, CacheWrite1h: 60}, &aCacheCreation{Ephemeral1h: 40})
}

func TestAnthropicCacheSplitUnreportedDeltaKeepsHour(t *testing.T) {
	before := splitUsage(t, `{"cache_creation_input_tokens":20,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":20}}`)
	start := splitUsage(t, `{"cache_creation_input_tokens":40,"cache_creation":{"ephemeral_5m_input_tokens":25,"ephemeral_1h_input_tokens":15}}`)
	delta := splitUsage(t, `{"cache_creation_input_tokens":40}`)
	var client Usage
	client.add(start.plus(before, false))
	client.add(delta.plus(before, true))
	if client.CacheWrite != 60 || client.CacheWrite1h != 35 {
		t.Fatalf("delta without a split changed known writes: got %d/%d, want 60/35", client.CacheWrite, client.CacheWrite1h)
	}
	wantUsageSplit(t, client, &aCacheCreation{Ephemeral5m: 25, Ephemeral1h: 35})
}

func TestAnthropicCacheSplitZeroDeltaKeepsWrites(t *testing.T) {
	s := newSniffer(provider.Anthropic, "text/event-stream")
	s.parse([]byte(`{"type":"message_start","message":{"usage":{"cache_creation_input_tokens":30}}}`))
	s.parse([]byte(`{"type":"message_delta","usage":{"cache_creation_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0}}}`))
	if s.u.CacheWrite != 30 {
		t.Fatalf("zero delta erased cache writes: got %d, want 30", s.u.CacheWrite)
	}
	wantUsageSplit(t, s.u, &aCacheCreation{Ephemeral5m: 30})
}

func TestAnthropicCacheSplitZeroDeltaKeepsEarlierRound(t *testing.T) {
	before := Usage{CacheWrite: 30}
	start := aUsage{CacheCreationInputTokens: 20, CacheCreation: &aCacheCreation{Ephemeral5m: 20}}.usage()
	delta := aUsage{CacheCreation: &aCacheCreation{}}.usage()
	var client Usage
	client.add(start.plus(before, false))
	if client.CacheWrite != 50 {
		t.Fatalf("message_start cache writes = %d, want 50", client.CacheWrite)
	}
	client.add(delta.plus(before, true))
	if client.CacheWrite != 50 {
		t.Fatalf("zero delta after earlier round reduced cache writes: got %d, want 50", client.CacheWrite)
	}
}

func TestOtherUsageDoesNotInventAnthropicSplit(t *testing.T) {
	var chat cUsage
	if err := json.Unmarshal([]byte(`{"prompt_tokens":100,"completion_tokens":7,"prompt_tokens_details":{"cache_write_tokens":40}}`), &chat); err != nil {
		t.Fatal(err)
	}
	var responses rUsage
	if err := json.Unmarshal([]byte(`{"input_tokens":100,"output_tokens":7,"input_tokens_details":{"cache_write_tokens":40}}`), &responses); err != nil {
		t.Fatal(err)
	}
	for _, u := range []Usage{chat.usage(), responses.usage()} {
		if u.CacheWrite != 40 {
			t.Fatalf("fixture has no cache writes: %+v", u)
		}
		wantUsageSplit(t, u, nil)
		var total Usage
		total.add(u)
		wantUsageSplit(t, total, nil)
		wantUsageSplit(t, total.plus(u, false), nil)
	}
}
