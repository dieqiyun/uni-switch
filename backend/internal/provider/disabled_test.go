package provider

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

func TestDisabledGlob(t *testing.T) {
	for _, c := range []struct {
		pat, s string
		ok     bool
	}{
		{"*fable*", "claude-fable-5", true},
		{"*fable*", "anthropic/claude-fable-5", true},
		{"claude-fable-5", "claude-fable-5", true},
		{"claude-fable-5", "claude-fable-5-mini", false},
		{"claude-fable-?", "claude-fable-5", true},
		{"anthropic/*", "anthropic/claude-fable-5", true},
		{"anthropic/*", "openrouter/anthropic/claude-fable-5", false},
		{"*", "", true},
		{"a*b*c", "aXbYc", true},
		{"a*b*c", "aXbY", false},
	} {
		if got := globMatch(c.pat, c.s); got != c.ok {
			t.Errorf("%q ~ %q: %v", c.pat, c.s, got)
		}
	}
	if pat, ok := matchDisabled([]string{" ", "*FABLE*"}, namesOf("openrouter", "anthropic/claude-fable-5")...); !ok || pat != "*FABLE*" {
		t.Fatal(pat, ok)
	}
	// an exact id matches the bare model, provider/model and the last segment
	for _, pat := range []string{"claude-fable-5", "openrouter/anthropic/claude-fable-5", "anthropic/claude-fable-5"} {
		if !DisabledBy(pat, "openrouter", "anthropic/claude-fable-5") {
			t.Error(pat)
		}
	}
	if DisabledBy("anthropic/*", "openrouter", "x") {
		t.Error("another provider's model")
	}
}

func disabledHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	for _, p := range []Provider{
		{ID: "a", Name: "A", Key: "k", Chat: "http://a.invalid/v1", Models: []string{"claude-fable-5", "m1"}},
		{ID: "b", Name: "B", Key: "k", Chat: "http://b.invalid/v1", Models: []string{"claude-fable-5-mini"}},
	} {
		if err := Save(p); err != nil {
			t.Fatal(err)
		}
	}
}

// A disabled model is left out of the catalog and of every group's
// members; a group left with none is told apart from an empty one; with
// nothing disabled, nothing changes.
func TestDisabledLeftOutOfTheCatalog(t *testing.T) {
	disabledHome(t)
	ids := func() []string {
		var out []string
		for _, e := range Catalog() {
			out = append(out, e.ID)
		}
		return out
	}
	before := ids()
	for _, id := range []string{"a/claude-fable-5", "b/claude-fable-5-mini"} {
		if !slices.Contains(before, id) {
			t.Fatalf("before: %v", before)
		}
	}
	if err := DisableModels("*fable*"); err != nil {
		t.Fatal(err)
	}
	after := ids()
	for _, id := range []string{"a/claude-fable-5", "b/claude-fable-5-mini"} {
		if slices.Contains(after, id) {
			t.Fatalf("%s still listed: %v", id, after)
		}
	}
	if !slices.Contains(after, "a/m1") {
		t.Fatalf("after: %v", after)
	}
	if hits := DisabledHits("*fable*"); !slices.Equal(hits, []string{"a/claude-fable-5", "b/claude-fable-5-mini"}) {
		t.Fatal(hits)
	}
	if pat, ok := BareDisabled("claude-fable-5"); !ok || pat != "*fable*" {
		t.Fatal(pat, ok)
	}
	// the same pattern twice, or differing only in case, is kept once
	if err := DisableModels("*FABLE*", " m9 "); err != nil {
		t.Fatal(err)
	}
	if got := DisabledModels(); !slices.Equal(got, []string{"*fable*", "m9"}) {
		t.Fatal(got)
	}
	if missing, err := EnableModels("*Fable*", "m9", "nope"); err != nil || !slices.Equal(missing, []string{"nope"}) {
		t.Fatal(missing, err)
	}
	if got := ids(); !slices.Equal(got, before) {
		t.Fatalf("enabled again: %v, was %v", got, before)
	}
}

// settings.json that can't be read is never taken for "none disabled": the
// list read last stays, and with none read, every model is in doubt and
// nothing is written over the file.
func TestDisabledUnreadSettings(t *testing.T) {
	disabledHome(t)
	if err := DisableModels("m1"); err != nil {
		t.Fatal(err)
	}
	if _, off := ModelDisabled("a", "m1"); !off {
		t.Fatal("not disabled")
	}
	path := settings.Path()
	os.Remove(path + ".bak")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.LoadChecked(); err == nil {
		t.Skip("settings recovered the file; nothing unread to test")
	}
	if _, off := ModelDisabled("a", "m1"); !off || DisabledUnknown() {
		t.Fatal("the last list read was dropped")
	}
	if err := SetDisabledModels(nil); err == nil {
		t.Fatal("wrote a list over a file nobody can read")
	}
	// none read before: unknown
	lastDisabled.Lock()
	lastDisabled.list, lastDisabled.read = nil, false
	lastDisabled.Unlock()
	if !DisabledUnknown() {
		t.Fatal("an unread list taken for none")
	}
	if b, _ := os.ReadFile(path); string(b) != "{not json" {
		t.Fatal("the file was written over", string(b))
	}
}
