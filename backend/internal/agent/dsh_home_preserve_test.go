package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The home layer can hold a newer choice than the profiles. Refreshing
// uni-switch's catalog, or only changing effort, must not select their old model.
func TestDshHomePickSurvivesSyncAndEffort(t *testing.T) {
	for _, action := range []string{"sync", "effort"} {
		t.Run(action, func(t *testing.T) {
			home, dir, web := dshRouteHome(t)
			if err := os.WriteFile(web, []byte("[]\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			a := dsh(home)
			if err := a.Field("model").Set("uni-switch/deepseek/pro"); err != nil {
				t.Fatal(err)
			}
			patch := filepath.Join(dir, "cordis.patch.yml")
			own := "# My shared choice\n- id: agent-default-model\n  config:\n    provider: deepseek-official\n    model: deepseek-v4-flash\n    reasoningEffort: low\n"
			if err := os.WriteFile(patch, []byte(own), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := a.Field("model").Get(); got != "deepseek-v4-flash" {
				t.Fatalf("before: %q", got)
			}
			var err error
			wantEffort := "low"
			if action == "sync" {
				err = a.Sync()
			} else {
				wantEffort = "max"
				err = a.Field("effort").Set(wantEffort)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := a.Field("model").Get(); got != "deepseek-v4-flash" {
				t.Fatalf("%s changed the home model to %q", action, got)
			}
			if got := a.Field("effort").Get(); got != wantEffort {
				t.Fatalf("%s: effort=%q, want %q", action, got, wantEffort)
			}
			b, err := os.ReadFile(patch)
			if err != nil {
				t.Fatal(err)
			}
			if action == "sync" && string(b) != own {
				t.Fatalf("catalog sync rewrote the user's choice:\n%s", b)
			}
			// A deliberate model pick still reaches the home layer, and
			// taking uni-switch out restores the user's original entry.
			if err := a.Field("model").Set("uni-switch/deepseek/flash"); err != nil {
				t.Fatal(err)
			}
			if got := a.Field("model").Get(); got != "uni-switch/deepseek/flash" {
				t.Fatalf("explicit pick: %q", got)
			}
			if err := a.Field("model").Set(""); err != nil {
				t.Fatal(err)
			}
			if b, err := os.ReadFile(patch); err != nil || string(b) != own {
				t.Fatalf("restore: %v\n%s", err, b)
			}
		})
	}
}

// Desktop 0.2's applyEntryPatches replaces only the fields supplied by a
// patch. disabled:false has no config, so the profile's model and route
// remain effective, and a model pick must not add configs to this layer.
func TestDshHomeMetadataKeepsProfileConfig(t *testing.T) {
	home, dir, web := dshRouteHome(t)
	if err := os.WriteFile(web, []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := dsh(home)
	if err := a.Field("model").Set("uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	patch := filepath.Join(dir, "cordis.patch.yml")
	own := "# Enable the bundled plugins\n- id: llm-pi-ai\n  disabled: false\n- id: agent-default-model\n  disabled: false\n"
	if err := os.WriteFile(patch, []byte(own), 0o644); err != nil {
		t.Fatal(err)
	}
	if !a.Joined() || a.Field("model").Get() != "uni-switch/deepseek/pro" || a.Check() != "" {
		t.Fatalf("metadata hid the profile: joined=%v, model=%q, check=%q", a.Joined(), a.Field("model").Get(), a.Check())
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := a.Field("model").Set("uni-switch/deepseek/flash"); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(patch); err != nil || string(b) != own {
		t.Fatalf("metadata patch gained a model or route: %v\n%s", err, b)
	}
	if err := a.Field("model").Set(""); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(patch); err != nil || string(b) != own || strings.Contains(string(b), "config:") {
		t.Fatalf("disconnect changed the metadata: %v\n%s", err, b)
	}
}
