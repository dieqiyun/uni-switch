package cli

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// patience= sets how long a request to the provider alone is kept while it
// is busy (zoecats on Discord): a time up to four minutes, off, or default;
// anything else is refused and leaves what was set.
func TestPatiencePair(t *testing.T) {
	var p provider.Provider
	for _, c := range []struct {
		v    string
		want int
	}{
		{"2m", 120}, {"90", 90}, {"45s", 45}, {"4m", 240}, {"off", provider.PatienceOff}, {"0", provider.PatienceOff}, {"default", 0},
	} {
		if err := applyPairs(&p, []string{"patience=" + c.v}); err != nil || p.Patience != c.want {
			t.Fatalf("patience=%s: %v, got %d, want %d", c.v, err, p.Patience, c.want)
		}
	}
	p.Patience = 30
	for _, bad := range []string{"5m", "-3", "soon", "1h"} {
		if err := applyPairs(&p, []string{"patience=" + bad}); err == nil || p.Patience != 30 {
			t.Fatalf("patience=%s taken: %v, %d", bad, err, p.Patience)
		}
	}
}
