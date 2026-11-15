package provider

import (
	"path/filepath"
	"testing"
)

// Removing many keys at once (361 on Discord: hundreds of keys, some dead):
// those named go in one save, the first gives its place to the next key on
// that stays, and removing every key in use is refused, leaving them all.
func TestRemoveKeys(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	if err := Save(Provider{ID: "many", Name: "Many", Chat: "https://example.invalid/v1", Key: "k1", KeyName: "first",
		Keys: []KeyAccount{{Key: "k2", Off: true}, {Key: "k3"}, {Key: "k4", Name: "four"}, {Key: "k5", Off: true}}}); err != nil {
		t.Fatal(err)
	}
	keys := func() (out []string) {
		p, _ := Find("many")
		for _, k := range p.KeyList() {
			out = append(out, k.ID)
		}
		return out
	}
	// the first and an off one: k3, the next on, is first
	n, err := RemoveKeys("many", []string{KeyID("k1"), KeyID("k2"), "nosuchkey"})
	if err != nil || n != 2 {
		t.Fatalf("removed %d: %v", n, err)
	}
	p, _ := Find("many")
	if p.Key != "k3" || p.KeyName != "" || len(p.Keys) != 2 || p.Keys[0].Key != "k4" || p.Keys[0].Name != "four" || p.Keys[1].Key != "k5" || !p.Keys[1].Off {
		t.Fatalf("left %q %+v", p.Key, p.Keys)
	}
	// every key in use: refused, nothing removed
	before := keys()
	if _, err := RemoveKeys("many", []string{KeyID("k3"), KeyID("k4")}); err == nil {
		t.Fatal("removed every key in use")
	}
	if after := keys(); len(after) != len(before) {
		t.Fatalf("a refused removal removed: %v → %v", before, after)
	}
	// none it has
	if _, err := RemoveKeys("many", []string{"nosuchkey"}); err == nil {
		t.Fatal("no key removed: no error")
	}
	// an off one alone leaves the first as it is
	if n, err := RemoveKeys("many", []string{KeyID("k5")}); err != nil || n != 1 {
		t.Fatalf("removed %d: %v", n, err)
	}
	if p, _ := Find("many"); p.Key != "k3" || len(p.Keys) != 1 {
		t.Fatalf("left %q %+v", p.Key, p.Keys)
	}
}
