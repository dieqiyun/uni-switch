package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// uni-switch model spec (Dani Ariyanto on Discord): a model nothing knows is
// shown unknown on each part, takes a whole spec in one command, shows it
// as the user's, is listed by uni-switch model specs, and --reset puts it back.
// A bad number, level, vision answer or flag is refused with the usage
// and writes nothing; --help anywhere prints the usage and writes nothing.
func TestModelSpecCmd(t *testing.T) {
	groupsHome(t)
	files := func() string {
		var out string
		for _, f := range []string{"settings.json", "providers.json"} {
			b, _ := os.ReadFile(filepath.Join(settings.Dir(), f))
			out += string(b)
		}
		return out
	}
	out, err := runOut(t, "model", "spec", "a/only-a")
	if err != nil || strings.Count(out, "neither the vendor's list nor models.dev says") != 4 {
		t.Fatalf("unknown spec: %q %v", out, err)
	}
	if out, err = runOut(t, "model", "specs"); err != nil || !strings.Contains(out, "no model has a spec of yours yet") {
		t.Fatalf("empty list: %q %v", out, err)
	}
	if out, err = runOut(t, "model", "spec", "a/only-a", "--context", "128k", "--output=16k", "--vision", "on", "--reasoning", "low-high"); err != nil {
		t.Fatalf("set: %q %v", out, err)
	}
	for _, want := range []string{"saved", "128k", "16k", "on", "low, medium, high"} {
		if !strings.Contains(out, want) && !strings.Contains(out, strings.ReplaceAll(want, ", ", ",")) {
			t.Errorf("set shows no %q: %q", want, out)
		}
	}
	if strings.Count(out, "yours · --") != 4 {
		t.Errorf("each part not said to be the user's: %q", out)
	}
	if out, err = runOut(t, "model", "specs"); err != nil || !strings.Contains(out, "a/only-a") || !strings.Contains(out, "vision on") || !strings.Contains(out, "reasoning low,medium,high") {
		t.Fatalf("list: %q %v", out, err)
	}

	before := files()
	for _, bad := range [][]string{
		{"model", "spec", "a/only-a", "--context", "abc"},
		{"model", "spec", "a/only-a", "--context", "0"},
		{"model", "spec", "a/only-a", "--output", "-5"},
		{"model", "spec", "a/only-a", "--reasoning", "turbo"},
		{"model", "spec", "a/only-a", "--reasoning", "high-low"},
		{"model", "spec", "a/only-a", "--context", "1m", "--reasoning", "bogus"},
		{"model", "spec", "a/only-a", "--vision", "maybe"},
		{"model", "spec", "a/only-a", "--frobnicate", "1"},
		{"model", "spec", "a/only-a", "--context"},
		{"model", "spec", "a/only-a", "128k"},
		{"model", "spec", "a/only-a", "--reset", "--context", "1m"},
		{"model", "spec", "a/nope", "--context", "1m"},
		{"model", "spec", "a/*", "--vision", "on"},
		{"model", "spec", "*/only-a", "--context", "1m"},
		{"model", "spec", "--context", "1m"},
	} {
		if out, err := runOut(t, bad...); err == nil {
			t.Errorf("took %v: %q", bad, out)
		}
		if files() != before {
			t.Fatalf("a refused %v wrote", bad)
		}
	}
	for _, help := range [][]string{
		{"model", "spec", "--help"},
		{"model", "spec", "a/only-a", "--help"},
		{"model", "spec", "a/only-a", "--context", "--help"},
		{"model", "specs", "--help"},
	} {
		if out, err := runOut(t, help...); err != nil || !strings.Contains(out, "say any of them: --context 200000 or 1m") {
			t.Errorf("%v: %q %v", help, out, err)
		}
		if files() != before {
			t.Fatalf("%v wrote", help)
		}
	}

	if out, err = runOut(t, "model", "spec", "a/only-a", "--reset"); err != nil || !strings.Contains(out, "context, output, vision, reasoning") {
		t.Fatalf("reset: %q %v", out, err)
	}
	if out, err = runOut(t, "model", "specs"); err != nil || !strings.Contains(out, "no model has a spec of yours yet") {
		t.Fatalf("list after reset: %q %v", out, err)
	}
	if _, err = runOut(t, "model", "spec", "a/only-a", "--reset"); err == nil || !strings.Contains(err.Error(), "no spec of yours to reset") {
		t.Fatalf("second reset: %v", err)
	}
}
