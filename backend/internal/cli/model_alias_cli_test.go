package cli

import (
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// uni-switch model alias counts a model's usage under another id, shows it,
// lists it under aliases and takes it off with --reset; it refuses a
// model aliased to itself, a loop, and a flag taken as a model id.
func TestModelAliasCmd(t *testing.T) {
	groupsHome(t)
	out, err := runOut(t, "model", "aliases")
	if err != nil || !strings.Contains(out, "no model is counted under another id yet") {
		t.Fatalf("empty list: %q %v", out, err)
	}
	if out, err = runOut(t, "model", "alias", "deepseek-v4.1-flash", "deepseek-flash"); err != nil || !strings.Contains(out, "counted as deepseek-flash") {
		t.Fatalf("set: %q %v", out, err)
	}
	if got := settings.Load().UsageAliases; got["deepseek-v4.1-flash"] != "deepseek-flash" {
		t.Fatalf("settings: %v", got)
	}
	if out, err = runOut(t, "model", "alias", "deepseek/deepseek-v4.1-flash"); err != nil || !strings.Contains(out, "deepseek-v4.1-flash") || !strings.Contains(out, "counted as deepseek-flash") {
		t.Fatalf("show: %q %v", out, err)
	}
	if out, err = runOut(t, "model", "aliases"); err != nil || !strings.Contains(out, "deepseek-v4.1-flash  → deepseek-flash") {
		t.Fatalf("list: %q %v", out, err)
	}
	for _, bad := range [][]string{
		{"model", "alias", "deepseek-flash", "DeepSeek_Flash"},
		{"model", "alias", "deepseek-flash", "deepseek-v4.1-flash"},
		{"model", "alias", "deepseek-flash", "--force"},
	} {
		if _, err := runOut(t, bad...); err == nil {
			t.Fatalf("took %v", bad)
		}
	}
	if out, err = runOut(t, "model", "alias", "--help"); err != nil || !strings.Contains(out, "uni-switch model alias <model> <name>") {
		t.Fatalf("--help: %q %v", out, err)
	}
	if got := settings.Load().UsageAliases; len(got) != 1 {
		t.Fatalf("a refused alias was written: %v", got)
	}
	if out, err = runOut(t, "model", "alias", "deepseek-v4.1-flash", "--reset"); err != nil || !strings.Contains(out, "its own id again") {
		t.Fatalf("reset: %q %v", out, err)
	}
	if got := settings.Load().UsageAliases; got != nil {
		t.Fatalf("after reset: %v", got)
	}
	if out, err = runOut(t, "model", "alias", "deepseek-v4.1-flash"); err != nil || !strings.Contains(out, "counted under its own id") {
		t.Fatalf("show after reset: %q %v", out, err)
	}
}
