package cli

import (
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// uni-switch model id lists a provider's model by an id of the user's, shows
// it, lists every one under ids and takes it off with --reset; it refuses
// a flag or a bad id taken as one, and a model the provider doesn't have.
func TestModelIDCmd(t *testing.T) {
	groupsHome(t)
	out, err := runOut(t, "model", "ids")
	if err != nil || !strings.Contains(out, "no model is listed by an id of yours yet") {
		t.Fatalf("empty list: %q %v", out, err)
	}
	if out, err = runOut(t, "model", "id", "a/only-a", "solo"); err != nil || !strings.Contains(out, "a/solo") || !strings.Contains(out, "still asked for only-a") {
		t.Fatalf("set: %q %v", out, err)
	}
	if got := settings.Load().ModelIDs; got["a/solo"] != "only-a" || len(got) != 1 {
		t.Fatalf("settings: %v", got)
	}
	p, err := provider.Find("a")
	if err != nil {
		t.Fatal(err)
	}
	if got := provider.UpstreamName(*p, "solo"); got != "only-a" {
		t.Fatalf("a/solo is asked for as %s", got)
	}
	for _, ref := range []string{"a/solo", "a/only-a"} {
		if out, err = runOut(t, "model", "id", ref); err != nil || !strings.Contains(out, "a/solo") || !strings.Contains(out, "asked for only-a") {
			t.Fatalf("show %s: %q %v", ref, out, err)
		}
	}
	if out, err = runOut(t, "model", "ids"); err != nil || !strings.Contains(out, "a/solo") || !strings.Contains(out, "only-a") {
		t.Fatalf("list: %q %v", out, err)
	}
	for _, bad := range [][]string{
		{"model", "id", "a/solo", "--force"},
		{"model", "id", "a/solo", "two words"},
		{"model", "id", "a/nope", "x"},
		{"model", "id", "solo", "x"},
	} {
		if _, err := runOut(t, bad...); err == nil {
			t.Fatalf("took %v", bad)
		}
	}
	if out, err = runOut(t, "model", "id", "--help"); err != nil || !strings.Contains(out, "uni-switch model id <provider/model> <id>") {
		t.Fatalf("--help: %q %v", out, err)
	}
	if got := settings.Load().ModelIDs; len(got) != 1 {
		t.Fatalf("a refused id was written: %v", got)
	}
	if out, err = runOut(t, "model", "id", "a/solo", "--reset"); err != nil || !strings.Contains(out, "its own id again") {
		t.Fatalf("reset: %q %v", out, err)
	}
	if got := settings.Load().ModelIDs; got != nil {
		t.Fatalf("after reset: %v", got)
	}
	if out, err = runOut(t, "model", "id", "a/only-a"); err != nil || !strings.Contains(out, "listed by its own id") {
		t.Fatalf("show after reset: %q %v", out, err)
	}
}
