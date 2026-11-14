package gateway

import (
	"testing"
	"time"
)

// The bucket tolerates a burst of wrong remote keys, then turns the next one
// away until a token refills, on an injected clock so the test is not timed.
func TestAuthThrottleBurstThenRefills(t *testing.T) {
	resetAuthThrottle()
	t.Cleanup(resetAuthThrottle)
	cur := time.Unix(1_700_000_000, 0)
	authThrottle.Lock()
	authThrottle.now = func() time.Time { return cur }
	authThrottle.Unlock()

	for i := 0; i < authFailBurst; i++ {
		if over, _ := tooManyAuthFails(); over {
			t.Fatalf("turned away within the burst, at try %d", i+1)
		}
	}
	over, retry := tooManyAuthFails()
	if !over || retry <= 0 {
		t.Fatalf("one past the burst: over=%v retry=%v, want over with a wait", over, retry)
	}
	// Still clamped a moment later.
	cur = cur.Add(authFailRefill / 2)
	if over, _ := tooManyAuthFails(); !over {
		t.Fatal("let a wrong key through before a token refilled")
	}
	// A full refill period frees exactly one token.
	cur = cur.Add(authFailRefill)
	if over, _ := tooManyAuthFails(); over {
		t.Fatal("still clamped after a token refilled")
	}
	if over, _ := tooManyAuthFails(); !over {
		t.Fatal("let a second wrong key through on one refilled token")
	}
}
