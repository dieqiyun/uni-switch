package codexcat

import "testing"

// A model's default in the catalog is the effort new sessions are set to,
// or the nearest level it takes; Default (unset)
// keeps DefaultEffort, and so does a level no rank knows.
func TestModelDefault(t *testing.T) {
	five := []string{"low", "medium", "high", "xhigh", "max"}
	group := []string{"low", "medium", "high"}
	for _, c := range []struct {
		levels []string
		chosen string
		got    string
	}{
		{five, "xhigh", "xhigh"},
		{group, "xhigh", "high"},
		{group, "", DefaultEffort(group)},
		{five, "", DefaultEffort(five)},
		{group, "bogus", DefaultEffort(group)},
	} {
		if g := ModelDefault(c.levels, c.chosen); g != c.got {
			t.Errorf("ModelDefault(%v, %q) = %q, want %q", c.levels, c.chosen, g, c.got)
		}
	}
}
