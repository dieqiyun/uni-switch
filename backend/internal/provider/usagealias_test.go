package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

func aliasHome(t *testing.T) {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	lastAliases.Store(nil)
	t.Cleanup(func() { lastAliases.Store(nil) })
}

// uni-switch model alias: off by default, a model counted under another id,
// matched however the id is spelt, and --reset gives it its own id back.
func TestUsageAliasSetAndReset(t *testing.T) {
	aliasHome(t)
	if UsageAlias() != nil {
		t.Fatal("an alias with none set")
	}
	changed, err := SetUsageAlias("deepseek-v4.1-flash", "deepseek-flash")
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	if got := settings.Load().UsageAliases; len(got) != 1 || got["deepseek-v4.1-flash"] != "deepseek-flash" {
		t.Fatalf("settings: %v", got)
	}
	a := UsageAlias()
	for _, id := range []string{"deepseek-v4.1-flash", "deepseek/deepseek-v4.1-flash", "DeepSeek-V4.1-Flash", "deepseek-v4-1-flash"} {
		if got := a(id); got != "deepseek-flash" {
			t.Fatalf("%s counted as %q", id, got)
		}
	}
	if got := a("deepseek-pro"); got != "deepseek-pro" {
		t.Fatalf("another model counted as %q", got)
	}
	if changed, err := SetUsageAlias("deepseek-v4.1-flash", "deepseek-flash"); err != nil || changed {
		t.Fatal("set again", changed, err)
	}
	// another spelling of the same model takes its place, not a second entry
	if _, err := SetUsageAlias("DeepSeek-V4.1-Flash", "ds-flash"); err != nil {
		t.Fatal(err)
	}
	if got := settings.Load().UsageAliases; len(got) != 1 || got["DeepSeek-V4.1-Flash"] != "ds-flash" {
		t.Fatalf("respelt: %v", got)
	}
	if from, to, counted, ok, err := UsageAliasFor("deepseek-v4.1-flash"); err != nil || !ok || from != "DeepSeek-V4.1-Flash" || to != "ds-flash" || counted != "ds-flash" {
		t.Fatal(from, to, counted, ok, err)
	}
	if changed, err := SetUsageAlias("deepseek-v4.1-flash", ""); err != nil || !changed {
		t.Fatal("reset", changed, err)
	}
	if got := settings.Load().UsageAliases; got != nil {
		t.Fatalf("after reset: %v", got)
	}
	if UsageAlias() != nil {
		t.Fatal("an alias after reset")
	}
	if changed, err := SetUsageAlias("deepseek-v4.1-flash", ""); err != nil || changed {
		t.Fatal("reset again", changed, err)
	}
}

// A model aliased to itself, or one that would loop back through the
// others, is refused and nothing is written; a chain is followed to its end.
func TestUsageAliasRefusesSelfAndLoops(t *testing.T) {
	aliasHome(t)
	for _, to := range []string{"deepseek-flash", "DeepSeek_Flash", "deepseek/deepseek-flash"} {
		if _, err := SetUsageAlias("deepseek-flash", to); err == nil {
			t.Fatalf("took deepseek-flash → %s", to)
		}
	}
	for _, bad := range [][2]string{{"", "x"}, {"--help", "x"}, {"m", "--reset-ish"}, {"///", "x"}} {
		if _, err := SetUsageAlias(bad[0], bad[1]); err == nil {
			t.Fatalf("took %q → %q", bad[0], bad[1])
		}
	}
	if _, err := SetUsageAlias("a-model", "b-model"); err != nil {
		t.Fatal(err)
	}
	if _, err := SetUsageAlias("b-model", "c-model"); err != nil {
		t.Fatal(err)
	}
	_, err := SetUsageAlias("c-model", "a-model")
	if err == nil || !strings.Contains(err.Error(), "c-model → a-model → b-model → c-model") {
		t.Fatalf("loop: %v", err)
	}
	if got := settings.Load().UsageAliases; len(got) != 2 {
		t.Fatalf("a refused loop was written: %v", got)
	}
	a := UsageAlias()
	if a("a-model") != "c-model" || a("b-model") != "c-model" || a("c-model") != "c-model" {
		t.Fatal(a("a-model"), a("b-model"), a("c-model"))
	}
}

// A loop written into settings.json by hand counts each model under its
// own id rather than spinning or picking one.
func TestUsageAliasHandWrittenLoop(t *testing.T) {
	a := UsageAliasIn(map[string]string{"x-model": "y-model", "y-model": "x-model", "p-model": "q-model", "self": "SELF"})
	if a("x-model") != "x-model" || a("y-model") != "y-model" || a("p-model") != "q-model" || a("self") != "self" {
		t.Fatal(a("x-model"), a("y-model"), a("p-model"), a("self"))
	}
}

// A settings.json that can't be read is unknown, not "no aliases": usage
// keeps the aliases last read, and nothing is written over the file.
func TestUsageAliasUnreadSettings(t *testing.T) {
	aliasHome(t)
	if _, err := SetUsageAlias("deepseek-v4.1-flash", "deepseek-flash"); err != nil {
		t.Fatal(err)
	}
	if a := UsageAlias(); a == nil || a("deepseek-v4.1-flash") != "deepseek-flash" {
		t.Fatal("not read")
	}
	if err := os.WriteFile(settings.Path(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if a := UsageAlias(); a == nil || a("deepseek-v4.1-flash") != "deepseek-flash" {
		t.Fatal("an unread settings.json dropped the aliases")
	}
	if _, err := SetUsageAlias("m-one", "m-two"); err == nil {
		t.Fatal("wrote over an unread settings.json")
	}
	if b, _ := os.ReadFile(settings.Path()); string(b) != "{not json" {
		t.Fatalf("settings.json rewritten: %s", b)
	}
	if _, _, _, _, err := UsageAliasFor("deepseek-v4.1-flash"); err == nil {
		t.Fatal("UsageAliasFor read an unread file as none")
	}
}
