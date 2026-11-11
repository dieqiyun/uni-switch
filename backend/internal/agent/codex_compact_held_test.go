package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A model_context_window or model_auto_compact_token_limit in Codex's
// config.toml comes before the window and threshold uni-switch's catalog gives
// (zgjfjfl on Discord: 128000 there, Long conversations at 544K, and Codex
// compacted every conversation near 128K). Long conversations names each
// one, top-level or in the profile the config picks, and takes them out
// when asked, leaving the rest of the file as it was.
func TestCodexCompactHeldNamedAndTakenOut(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	cfg := `model = "uni-switch/relay/big"
model_provider = "uni-switch"
model_context_window = 128_000 # from a guide
profile = "work"

[profiles.work]
model_auto_compact_token_limit = 120000
model = "uni-switch/relay/big"

[profiles.other]
model_context_window = 64000
`
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	got := CodexCompactHeld(home)
	want := []CodexCompactHold{
		{Path: path, Key: "model_context_window", Value: "128000"},
		{Path: path, Key: "model_auto_compact_token_limit", Profile: "work", Value: "120000"},
	}
	if len(got) != len(want) {
		t.Fatalf("held = %+v; want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("held[%d] = %+v; want %+v", i, got[i], want[i])
		}
	}

	if err := DropCodexCompactHolds(home); err != nil {
		t.Fatal(err)
	}
	if h := CodexCompactHeld(home); len(h) != 0 {
		t.Fatalf("after taking them out, held = %+v; want none", h)
	}
	b, _ := os.ReadFile(path)
	out := string(b)
	for _, keep := range []string{`model_provider = "uni-switch"`, `profile = "work"`, "[profiles.work]", "[profiles.other]", "model_context_window = 64000"} {
		if !strings.Contains(out, keep) {
			t.Fatalf("taking them out lost %q:\n%s", keep, out)
		}
	}
	if strings.Contains(out, "128_000") || strings.Contains(out, "120000") {
		t.Fatalf("still there:\n%s", out)
	}
}

// With none of them, nothing is named, and a config with no file is none.
func TestCodexCompactHeldNone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	if h := CodexCompactHeld(home); len(h) != 0 {
		t.Fatalf("no config: held = %+v", h)
	}
	if err := DropCodexCompactHolds(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); err == nil {
		t.Fatal("taking nothing out wrote a config")
	}
}
