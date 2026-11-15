package cli

import (
	"strings"
	"testing"
)

// uni-switch provider set free=yes counts every model of a provider free for
// Free first (jiabubu on Discord: ModelScope's free daily quota), show
// says so, free=no takes it back, and any other value is refused and
// leaves the saved one alone.
func TestProviderSetFree(t *testing.T) {
	groupsHome(t)
	if _, err := printed(t, func() error { return providerCmd([]string{"provider", "set", "a", "free=yes"}) }); err != nil {
		t.Fatal(err)
	}
	if p := mustFind(t, "a"); !p.CountFree || !p.FreeModel("whatever") {
		t.Fatalf("free=yes: %+v", p)
	}
	out, err := printed(t, func() error { return providerCmd([]string{"provider", "a"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "counted free") {
		t.Fatalf("show doesn't say it is counted free:\n%s", out)
	}
	for _, v := range []string{"maybe", "", "--help"} {
		err := providerCmd([]string{"provider", "set", "a", "free=" + v})
		if err == nil || !strings.Contains(err.Error(), "free=yes|no") {
			t.Errorf("free=%q: %v", v, err)
		}
		if !mustFind(t, "a").CountFree {
			t.Errorf("a refused free=%q turned it off", v)
		}
	}
	if _, err := printed(t, func() error { return providerCmd([]string{"provider", "set", "a", "free=no"}) }); err != nil {
		t.Fatal(err)
	}
	if mustFind(t, "a").CountFree {
		t.Fatal("free=no left it on")
	}
}
