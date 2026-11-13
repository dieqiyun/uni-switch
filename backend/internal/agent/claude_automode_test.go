package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// claudeAutoModeHome is a sandbox HOME with a provider serving a Claude
// model the classifier runs on, one Claude Code refuses for it, and
// another vendor's, and Claude Code's settings.json holding before.
func claudeAutoModeHome(t *testing.T, before string) (*Agent, string, func(string) string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Chat: "https://relay.example/v1", Key: "k", Models: []string{"claude-haiku-4-5", "claude-opus-5-5", "deepseek-pro"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	env := func(k string) string { v, _ := edit.GetJSON(path, "env."+k); return v }
	return claude(home), path, env
}

// Claude Code's auto mode classifier can have a model of its own
// (CLAUDE_CODE_AUTO_MODE_MODEL, 2.1.302; Linka on Discord): only while
// uni-switch routes Claude Code, only one of uni-switch's models, never one Claude
// Code refuses to run its classifier on, and gone again with uni-switch.
func TestClaudeAutoModeModel(t *testing.T) {
	a, _, env := claudeAutoModeHome(t, `{}`)
	f := a.Field("auto_mode")
	if f == nil {
		t.Fatal("no auto_mode field")
	}
	if err := f.Set("relay/claude-haiku-4-5"); err == nil {
		t.Fatal("an auto mode model before Claude Code is routed should fail")
	}
	if len(f.Options(nil)) != 0 || f.Get() != "" {
		t.Fatalf("unrouted: %q %v", f.Get(), f.Options(nil))
	}
	if err := a.Field("model").Set("relay/deepseek-pro"); err != nil {
		t.Fatal(err)
	}
	// unset, Claude Code's own pick: uni-switch writes nothing for it
	if f.Get() != "" || env(claudeAutoModeEnv) != "" {
		t.Fatalf("auto mode should be unset: %v", a.Values())
	}
	opts := f.Options(nil)
	if !containsOption(opts, "relay/claude-haiku-4-5") || !containsOption(opts, "relay/deepseek-pro") {
		t.Fatalf("options miss uni-switch's models: %v", opts)
	}
	if containsOption(opts, "relay/claude-opus-5-5") {
		t.Fatalf("Opus 5.5 is offered, which Claude Code won't classify with: %v", opts)
	}
	if err := f.Set("relay/claude-opus-5-5"); err == nil || env(claudeAutoModeEnv) != "" {
		t.Fatalf("Opus 5.5 taken: %v %q", err, env(claudeAutoModeEnv))
	}
	if err := f.Set("claude-haiku-4-5"); err == nil || env(claudeAutoModeEnv) != "" {
		t.Fatalf("a model uni-switch doesn't serve taken: %v %q", err, env(claudeAutoModeEnv))
	}
	if err := f.Set("relay/claude-haiku-4-5"); err != nil {
		t.Fatal(err)
	}
	// written as Claude Code is given a model, with dashes
	if got := env(claudeAutoModeEnv); got != provider.ClaudeSpelled("relay/claude-haiku-4-5") {
		t.Fatalf("env %q", got)
	}
	if f.Get() != "relay/claude-haiku-4-5" {
		t.Fatalf("get %q", f.Get())
	}
	// a new main model leaves it be: it follows no field
	if err := a.Field("model").Set("relay/claude-haiku-4-5"); err != nil {
		t.Fatal(err)
	}
	if f.Get() != "relay/claude-haiku-4-5" {
		t.Fatalf("a new model moved auto mode: %v", a.Values())
	}
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if env(claudeAutoModeEnv) != "" {
		t.Fatal("clearing left the env behind")
	}
	if err := f.Set("relay/deepseek-pro"); err != nil {
		t.Fatal(err)
	}
	// back to Claude Code as installed: uni-switch's pick goes with it
	if err := a.Field("model").Set(""); err != nil {
		t.Fatal(err)
	}
	if env(claudeAutoModeEnv) != "" {
		t.Fatalf("reset left the auto mode model behind")
	}
	if err := f.Set(""); err != nil {
		t.Fatalf("clearing while unrouted: %v", err)
	}
}

// An auto mode model the user set before uni-switch routed Claude Code waits
// out of the env while uni-switch is wired in (it would name a model uni-switch
// doesn't serve) and comes back after; one they set while uni-switch is wired
// in is theirs, and uni-switch never takes it out.
func TestClaudeAutoModeUserValue(t *testing.T) {
	t.Run("before", func(t *testing.T) {
		a, path, env := claudeAutoModeHome(t, `{"env":{"CLAUDE_CODE_AUTO_MODE_MODEL":"claude-sonnet-4-6"}}`)
		if err := a.Field("model").Set("relay/deepseek-pro"); err != nil {
			t.Fatal(err)
		}
		if env(claudeAutoModeEnv) != "" || a.Field("auto_mode").Get() != "" {
			t.Fatalf("the user's own auto mode model stayed in while routed: %q", env(claudeAutoModeEnv))
		}
		if err := a.Field("auto_mode").Set("relay/claude-haiku-4-5"); err != nil {
			t.Fatal(err)
		}
		if err := a.Disconnect(); err != nil {
			t.Fatal(err)
		}
		if got := env(claudeAutoModeEnv); got != "claude-sonnet-4-6" {
			b, _ := os.ReadFile(path)
			t.Fatalf("the user's own auto mode model not back: %q\n%s", got, b)
		}
	})
	t.Run("while routed", func(t *testing.T) {
		a, _, env := claudeAutoModeHome(t, `{}`)
		if err := a.Field("model").Set("relay/deepseek-pro"); err != nil {
			t.Fatal(err)
		}
		v := "claude-sonnet-4-6"
		if err := edit.SetJSON(filepath.Join(os.Getenv("HOME"), ".claude", "settings.json"), edit.KV{Path: "env." + claudeAutoModeEnv, Value: v}); err != nil {
			t.Fatal(err)
		}
		// another main model, then Claude Code as installed
		if err := a.Field("model").Set("relay/claude-haiku-4-5"); err != nil {
			t.Fatal(err)
		}
		if env(claudeAutoModeEnv) != v {
			t.Fatalf("a new model took the user's auto mode model: %q", env(claudeAutoModeEnv))
		}
		if err := a.Field("model").Set(""); err != nil {
			t.Fatal(err)
		}
		if env(claudeAutoModeEnv) != v {
			t.Fatalf("stepping out took the user's auto mode model: %q", env(claudeAutoModeEnv))
		}
	})
	t.Run("set again before stepping out", func(t *testing.T) {
		a, _, env := claudeAutoModeHome(t, `{"env":{"CLAUDE_CODE_AUTO_MODE_MODEL":"claude-sonnet-4-6"}}`)
		if err := a.Field("model").Set("relay/deepseek-pro"); err != nil {
			t.Fatal(err)
		}
		v := "claude-haiku-4-5"
		if err := edit.SetJSON(filepath.Join(os.Getenv("HOME"), ".claude", "settings.json"), edit.KV{Path: "env." + claudeAutoModeEnv, Value: v}); err != nil {
			t.Fatal(err)
		}
		if err := a.Disconnect(); err != nil {
			t.Fatal(err)
		}
		if env(claudeAutoModeEnv) != v {
			t.Fatalf("the stash wrote over the user's newer auto mode model: %q", env(claudeAutoModeEnv))
		}
	})
}
