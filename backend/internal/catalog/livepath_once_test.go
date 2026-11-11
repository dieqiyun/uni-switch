package catalog

import (
	"path/filepath"
	"testing"
)

// A provider's list file is named once, not on each read of the list: a
// look at the providers reads it for every model.
func TestLivePathNamedOnce(t *testing.T) {
	if got, want := LivePath("openai"), filepath.Join(filepath.Dir(CachePath()), "models", "openai.json"); got != want {
		t.Fatalf("LivePath = %q, want %q", got, want)
	}
	if n := testing.AllocsPerRun(20, func() { LivePath("openai") }); n > 0 {
		t.Errorf("LivePath with the folders unchanged: %.0f allocations", n)
	}
}
