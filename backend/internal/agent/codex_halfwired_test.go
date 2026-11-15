package agent

import (
	"errors"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Tystem on Discord: Codex wouldn't load its config ("Model provider
// `uni-switch` not found") with model_provider = "uni-switch" and model =
// "deepseek-flash" left in config.toml and no [model_providers.uni-switch],
// and uni-switch showed Codex as not connected. Codex refuses the whole config
// so, not just an old thread: Check says so, and Sync writes the table
// back, at the top and for a profile alike.
func TestCodexProviderWithoutTable(t *testing.T) {
	for _, tc := range []struct{ name, config string }{
		{"top", "model_provider = \"uni-switch\"\nmodel = \"deepseek-flash\"\n"},
		{"profile", "profile = \"ds\"\n\n[profiles.ds]\nmodel_provider = \"uni-switch\"\nmodel = \"deepseek-flash\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, read := codexHome(t, "", tc.config)
			cx := codex(home)
			if msg := cx.Check(); !strings.Contains(msg, "[model_providers.uni-switch]") {
				t.Fatalf("check: %q", msg)
			}
			if err := cx.Sync(); err != nil {
				t.Fatal(err)
			}
			tb, err := edit.GetTOMLTable(cx.Path, "model_providers.uni-switch")
			if err != nil || tb["base_url"] != here(home).v1() || tb["wire_api"] != "responses" || tb["experimental_bearer_token"] == "" {
				t.Fatalf("table %v %v:\n%s", tb, err, read())
			}
			if msg := cx.Check(); strings.Contains(msg, "[model_providers.uni-switch]") {
				t.Fatalf("check after sync: %q", msg)
			}
		})
	}
}

// Every way uni-switch steps out as Codex's provider takes model_provider out
// with it and leaves the table: never the half state above.
func TestCodexSteppingOutLeavesNoHalfState(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  func(a *Agent) error
	}{
		{"disconnect", func(a *Agent) error { return a.Fields[0].Set("") }},
		{"unwire", func(a *Agent) error { return a.Unwire() }},
		{"own model", func(a *Agent) error { return a.Fields[0].Set("gpt-5.5") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, read := codexHome(t, "", "model = \"gpt-5.5\"\n")
			cx := codex(home)
			if err := cx.Fields[0].Set("fake/m1"); err != nil {
				t.Fatal(err)
			}
			if err := tc.out(cx); err != nil {
				t.Fatal(err)
			}
			if p, _ := edit.GetTOMLTop(cx.Path, "model_provider"); p == uniSwitchID {
				t.Fatalf("model_provider left:\n%s", read())
			}
			if tb, _ := edit.GetTOMLTable(cx.Path, "model_providers.uni-switch"); tb == nil {
				t.Fatalf("table gone, for the threads started on uni-switch:\n%s", read())
			}
		})
	}
}

// Connecting with no provider or subscription in uni-switch says what to do,
// with a code the GUI says it by in the reader's language.
func TestConnectWithNoModels(t *testing.T) {
	home, _ := codexHome(t, "", "model = \"gpt-5.5\"\n")
	if err := provider.Delete("fake"); err != nil {
		t.Fatal(err)
	}
	_, err := codex(home).ConnectHow()
	var nm *NoModelsError
	if !errors.As(err, &nm) || nm.Agent != "Codex" || !strings.Contains(err.Error(), "Add a provider or subscription in uni-switch first, then connect Codex") {
		t.Fatalf("%v", err)
	}
}
