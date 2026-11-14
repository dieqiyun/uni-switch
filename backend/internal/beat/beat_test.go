package beat

import (
	"testing"
	"time"
)

// Loops whose intervals are multiples of one another wake at one time: a
// 5s loop's every third beat is a 15s loop's, and its every sixth a 30s
// loop's.
func TestIntervalsMeet(t *testing.T) {
	now := time.Date(2026, 10, 26, 9, 0, 3, 200e6, time.UTC)
	for _, c := range []struct {
		every time.Duration
		want  time.Time
	}{
		{5 * time.Second, time.Date(2026, 10, 26, 9, 0, 5, 0, time.UTC)},
		{15 * time.Second, time.Date(2026, 10, 26, 9, 0, 15, 0, time.UTC)},
		{30 * time.Second, time.Date(2026, 10, 26, 9, 0, 30, 0, time.UTC)},
		{time.Minute, time.Date(2026, 10, 26, 9, 1, 0, 0, time.UTC)},
	} {
		if got := after(now, c.every); !got.Equal(c.want) {
			t.Errorf("after(%s, %s) = %s, want %s", now.Format(time.TimeOnly), c.every, got.Format(time.TimeOnly), c.want.Format(time.TimeOnly))
		}
	}
	// on a beat, the next is the one after it
	if got := after(time.Date(2026, 10, 26, 9, 0, 5, 0, time.UTC), 5*time.Second); !got.Equal(time.Date(2026, 10, 26, 9, 0, 10, 0, time.UTC)) {
		t.Errorf("the beat after one on 9:00:05 is %s", got.Format(time.TimeOnly))
	}
}

// A beat comes: a 10ms one well within 10s.
func TestNextFires(t *testing.T) {
	select {
	case <-Next(10 * time.Millisecond):
	case <-time.After(10 * time.Second):
		t.Fatal("a 10ms beat didn't come in 10s")
	}
}
