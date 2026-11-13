package provider

import (
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// desktopSubagentHome is a fresh home for uni-switch's files.
func desktopSubagentHome(t *testing.T) {
	t.Helper()
	isolate(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := Save(Provider{ID: "fake", Name: "Fake", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
}

// The model set for Desktop's subagents is kept trimmed, a bare name or a
// half id is refused, and "" clears it.
func TestDesktopSubagentModelRoundTrip(t *testing.T) {
	desktopSubagentHome(t)
	release := Hold()
	defer release()
	for _, id := range []string{" fake/m1 ", " group/workers ", "missing/m", ""} {
		if err := SetDesktopSubagentModel(id); err != nil {
			t.Fatal(err)
		}
		if got := DesktopSubagentModel(); got != strings.TrimSpace(id) || got != settings.Load().DesktopSubagentModel {
			t.Fatalf("model %q, stored %q, input %q", got, settings.Load().DesktopSubagentModel, id)
		}
	}
	for _, id := range []string{"gpt-6", "/m", "p/"} {
		if err := SetDesktopSubagentModel(id); err == nil {
			t.Errorf("accepted invalid model %q", id)
		}
		if DesktopSubagentModel() != "" {
			t.Error("invalid pick changed the model")
		}
	}
}

// A provider renamed after its model was set is read by its new id.
func TestDesktopSubagentModelRenamed(t *testing.T) {
	desktopSubagentHome(t)
	if err := SetDesktopSubagentModel("fake/m1"); err != nil {
		t.Fatal(err)
	}
	if err := Rename("fake", "renamed"); err != nil {
		t.Fatal(err)
	}
	if got := DesktopSubagentModel(); got != "renamed/m1" {
		t.Fatalf("model after rename %q, want renamed/m1", got)
	}
	if why := DesktopSubagentUnserved("fake/m1"); why != "" {
		t.Fatalf("renamed model unserved: %s", why)
	}
}

func TestDesktopSubagentEffortRoundTrip(t *testing.T) {
	desktopSubagentHome(t)
	release := Hold()
	defer release()
	for _, level := range append(append([]string{}, MemberEfforts...), " HIGH ", "") {
		if err := SetDesktopSubagentEffort(level); err != nil {
			t.Fatal(err)
		}
		if got := DesktopSubagentEffort(); got != strings.ToLower(strings.TrimSpace(level)) {
			t.Fatalf("effort %q, input %q", got, level)
		}
	}
	if err := SetDesktopSubagentEffort("high"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"hard", "ultra", "high:low"} {
		if err := SetDesktopSubagentEffort(bad); err == nil || DesktopSubagentEffort() != "high" {
			t.Fatalf("invalid pick %q changed the effort: %v", bad, err)
		}
	}
	for _, model := range []string{"fake/m1", "missing/m", ""} {
		if err := SetDesktopSubagentModel(model); err != nil || DesktopSubagentEffort() != "high" {
			t.Fatalf("model change lost effort: %v", err)
		}
	}
}
