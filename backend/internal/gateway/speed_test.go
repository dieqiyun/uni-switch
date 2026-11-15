package gateway

import (
	"io"
	"math"
	"net/http"

	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Speed lately is a decayed average: a sample counts half after
// speedHalfLife against those since, and a seat not measured for
// speedStale counts as not measured at all, so Speed first gives it a
// probe's turn instead of holding an old number against it.
func TestSpeedLatelyDecaysAndGoesStale(t *testing.T) {
	forgetSpeeds()
	t.Cleanup(forgetSpeeds)
	c := candidate{p: provider.Provider{ID: "v"}, rest: "v", model: "m"}
	t0 := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	noteSpeed(c, 1000, 0, 0, t0)
	if ttft, tps := speedOf(c, t0); ttft != 1000 || tps != 0 {
		t.Fatalf("one sample without a pace: %v %v", ttft, tps)
	}
	// a sample a half-life later counts double the first: (1000*0.5 + 4000)/1.5
	noteSpeed(c, 4000, 100, 1000, t0.Add(speedHalfLife))
	ttft, tps := speedOf(c, t0.Add(speedHalfLife))
	if math.Abs(ttft-3000) > 0.01 || tps != 100 {
		t.Fatalf("after a half-life: ttft %v tps %v, want 3000 and 100", ttft, tps)
	}
	// a reply that wasn't streamed tells nothing
	noteSpeed(c, 0, 50, 1000, t0.Add(speedHalfLife))
	if again, _ := speedOf(c, t0.Add(speedHalfLife)); again != ttft {
		t.Fatalf("an unstreamed reply moved the average: %v", again)
	}
	// stale: not measured lately
	if ttft, tps := speedOf(c, t0.Add(speedHalfLife+speedStale+time.Second)); ttft != 0 || tps != 0 {
		t.Fatalf("an hour and more on, still measured: %v %v", ttft, tps)
	}
	// another model of the same key is its own
	if ttft, _ := speedOf(candidate{p: c.p, rest: "v", model: "n"}, t0); ttft != 0 {
		t.Fatalf("another model shares the speed: %v", ttft)
	}
}

// streamed is a vendor that streams one short chat answer, its first
// content a few ms after the request so its time to first token is
// measured (in whole ms).
func streamed(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body)
	time.Sleep(3 * time.Millisecond)
	w.Header().Set("Content-Type", "text/event-stream")
	io.WriteString(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"}}]}\n\n")
	io.WriteString(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1,\"total_tokens\":4}}\n\n")
	io.WriteString(w, "data: [DONE]\n\n")
}
