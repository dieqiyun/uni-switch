package catalog

import (
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
)

// A provider's list file is named once, not on each read of the list: a
// look at the providers reads it for every model.
func TestLivePathNamedOnce(t *testing.T) {
	if got, want := LivePath("openai"), filepath.Join(filepath.Dir(CachePath()), "models", "openai.json"); got != want {
		t.Fatalf("LivePath = %q, want %q", got, want)
	}
	// Cache reads the current environment, which allocates on Windows.
	// Constructing the provider's path must add none of its own.
	reads := testing.AllocsPerRun(20, func() { appdir.Cache() })
	if n := testing.AllocsPerRun(20, func() { LivePath("openai") }); n > reads {
		t.Errorf("LivePath with the folders unchanged: %.0f allocations beyond cache folder reads", n-reads)
	}
}
