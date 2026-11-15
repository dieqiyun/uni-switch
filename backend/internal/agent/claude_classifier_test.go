package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// CyberCaius on Discord: after a switch in /model, the first auto mode
// classifier request went to the model before it. Claude Code names its
// sonnet tier for the classifier, and uni-switch's sonnet tier follows the
// main model only once Follow has run. Until then the classifier's model
// is the tier the new main model would give it; a tier, or an auto mode
// model, of the user's own is theirs as it is.
func TestClaudeClassifierFollowsTheSwitchedModel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"pro", "flash"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a := claude(home)
	if err := a.Field("model").Set("deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	env := func(k string) string { v, _ := edit.GetJSON(path, "env."+k); return v }
	tier := env(tierEnv("sonnet"))
	if tier != "deepseek/pro" {
		t.Fatalf("the sonnet tier doesn't follow the main model: %q", tier)
	}
	at := here(home)
	if got := claudeClassifierIn(at, tier); got != "" {
		t.Fatalf("main model unchanged, the classifier was moved to %q", got)
	}

	// /model saves the new main model; Follow hasn't run
	if err := edit.SetJSON(path, edit.KV{Path: "model", Value: "deepseek/flash"}); err != nil {
		t.Fatal(err)
	}
	if env(tierEnv("sonnet")) != tier {
		t.Fatal("the tier moved without Follow")
	}
	if got := claudeClassifierIn(at, tier); got != "deepseek/flash" {
		t.Fatalf("after /model the classifier goes to %q, want deepseek/flash", got)
	}
	if got := claudeClassifierIn(at, "deepseek/flash"); got != "" {
		t.Fatalf("a model that isn't the sonnet tier was moved to %q", got)
	}

	// an auto mode model of the user's own
	if err := edit.SetJSON(path, edit.KV{Path: "env." + claudeAutoModeEnv, Value: "deepseek/pro"}); err != nil {
		t.Fatal(err)
	}
	if got := claudeClassifierIn(at, tier); got != "" {
		t.Fatalf("the user's auto mode model was moved to %q", got)
	}
	if err := edit.SetJSON(path, edit.KV{Path: "env." + claudeAutoModeEnv, Value: nil}); err != nil {
		t.Fatal(err)
	}

	// a sonnet tier of the user's own
	if err := edit.SetJSON(path, edit.KV{Path: "model", Value: "deepseek/pro"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Field("sonnet").Set("deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	if err := edit.SetJSON(path, edit.KV{Path: "model", Value: "deepseek/flash"}); err != nil {
		t.Fatal(err)
	}
	if got := claudeClassifierIn(at, "deepseek/pro"); got != "" {
		t.Fatalf("the user's own sonnet tier was moved to %q", got)
	}

	// Follow has run: the tier is the new main model's, and nothing moves
	if err := a.Field("sonnet").Set(""); err != nil {
		t.Fatal(err)
	}
	if err := a.Follow(); err != nil {
		t.Fatal(err)
	}
	if got := env(tierEnv("sonnet")); got != "deepseek/flash" {
		t.Fatalf("Follow left the tier on %q", got)
	}
	if got := claudeClassifierIn(at, "deepseek/flash"); got != "" {
		t.Fatalf("after Follow the classifier was moved to %q", got)
	}
}
