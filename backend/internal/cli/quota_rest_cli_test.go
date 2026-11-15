package cli

import (
	"testing"
	"time"
)

// #1772: `uni-switch provider quota-rest <id> 7d` takes a rest as a person says
// it, and says it back the same way.
func TestRestMinutes(t *testing.T) {
	for in, want := range map[string]int{"default": 0, "off": 0, "0": 0, "90": 90, "30m": 30, "6h": 360, "7d": 7 * 24 * 60, "1h30m": 90} {
		if got, err := restMinutes(in); err != nil || got != want {
			t.Errorf("%q: %d %v, want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"soon", "30s", "d"} {
		if _, err := restMinutes(in); err == nil {
			t.Errorf("%q taken", in)
		}
	}
	for d, want := range map[time.Duration]string{7 * 24 * time.Hour: "7 days", 6 * time.Hour: "6 hours", 90 * time.Minute: "90 minutes"} {
		if got := restWords(d); got != want {
			t.Errorf("%v: %q, want %q", d, got, want)
		}
	}
}
