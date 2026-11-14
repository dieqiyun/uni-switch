package provider

import (
	"testing"
	"time"
)

func TestKeepAliveBackoff(t *testing.T) {
	for n, want := range map[int]time.Duration{1: time.Minute, 2: 2 * time.Minute, 3: 4 * time.Minute, 5: 16 * time.Minute, 6: 30 * time.Minute, 50: 30 * time.Minute} {
		if got := keepAliveBackoff(n); got != want {
			t.Errorf("keepAliveBackoff(%d) = %v, want %v", n, got, want)
		}
	}
}
