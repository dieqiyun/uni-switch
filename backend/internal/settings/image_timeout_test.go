package settings

import (
	"testing"
	"time"
)

// Settings' Image generation timeout (neighbads on X: a slow image
// endpoint needs more than 5 minutes) is kept from 1 to 60 minutes, and
// 0, which is what a settings.json without it reads as, is the 5 drawing
// was always given.
func TestImageTimeoutKeptAndBounded(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if w := Load().ImageWait(); w != 5*time.Minute {
		t.Fatalf("unset: %v", w)
	}
	for _, m := range []int{1, 30, 60} {
		if err := Save(Settings{ImageTimeout: m}); err != nil {
			t.Fatalf("%d: %v", m, err)
		}
		if s := Load(); s.ImageTimeout != m || s.ImageWait() != time.Duration(m)*time.Minute {
			t.Fatalf("%d read back as %d, %v", m, s.ImageTimeout, s.ImageWait())
		}
	}
	for _, m := range []int{-1, 61, 600} {
		if Save(Settings{ImageTimeout: m}) == nil {
			t.Fatalf("%d minutes accepted", m)
		}
	}
	if Load().ImageTimeout != 60 {
		t.Fatal("a refused value overwrote the saved one")
	}
	// a settings.json written by hand past the bounds still waits within them
	if w := (Settings{ImageTimeout: 600}).ImageWait(); w != time.Hour {
		t.Fatalf("600 waits %v", w)
	}
	if w := (Settings{ImageTimeout: -3}).ImageWait(); w != 5*time.Minute {
		t.Fatalf("-3 waits %v", w)
	}
}
