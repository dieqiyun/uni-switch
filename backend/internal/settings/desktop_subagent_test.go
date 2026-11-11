package settings

import (
	"strings"
	"testing"
)

// settings.json keeps the model set for Desktop's subagents trimmed and
// refuses a bare name.
func TestDesktopSubagentModelSetting(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, id := range []string{" fake/m1 ", " group/workers ", " "} {
		if err := Save(Settings{DesktopSubagentModel: id}); err != nil {
			t.Fatal(err)
		}
		if got := Load().DesktopSubagentModel; got != strings.TrimSpace(id) {
			t.Fatalf("saved model %q, want %q", got, strings.TrimSpace(id))
		}
	}
	if err := Save(Settings{DesktopSubagentModel: "gpt-6"}); err == nil || !strings.Contains(err.Error(), "the model for Claude Desktop's subagents") {
		t.Fatalf("bare model validation: %v", err)
	}
}

func TestDesktopSubagentEffortSetting(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := Save(Settings{DesktopSubagentEffort: " HIGH "}); err != nil {
		t.Fatal(err)
	}
	if got := Load().DesktopSubagentEffort; got != "high" {
		t.Fatalf("saved effort %q, want high", got)
	}
	for _, bad := range []string{"hard", "ultra"} {
		if err := Save(Settings{DesktopSubagentEffort: bad}); err == nil {
			t.Fatalf("accepted invalid effort %q", bad)
		}
	}
	if err := Save(Settings{}); err != nil || Load().DesktopSubagentEffort != "" {
		t.Fatalf("clearing effort: %v", err)
	}
}
