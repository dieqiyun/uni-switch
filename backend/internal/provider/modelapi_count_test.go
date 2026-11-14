package provider

import (
	"path/filepath"
	"testing"
)

// ModelAPI is asked of each model of every provider's list on the
// Providers page: one with no API set is answered without listing the
// protocols the provider speaks, and one set is answered as before.
func TestModelAPIListsNoProtocols(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	p := Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Anthropic: "http://127.0.0.1:1", Models: []string{"a", "b"}}
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	if err := SetModelAPI("relay/b", string(Anthropic)); err != nil {
		t.Fatal(err)
	}
	defer Hold()()
	if _, ok := p.ModelAPI("a"); ok {
		t.Fatal("a model with no API set has one")
	}
	if api, ok := p.ModelAPI("b"); !ok || api != Anthropic {
		t.Fatalf("the model set on Anthropic's API is on %q (%v)", api, ok)
	}
	if one := (Provider{ID: "relay", Chat: p.Chat}); func() bool { _, ok := one.ModelAPI("b"); return ok }() {
		t.Fatal("a provider with one API keeps a model set on another")
	}
	listed := allocatedBy(func() { _ = p.Speaks() })
	asked := allocatedBy(func() { _, _ = p.ModelAPI("a") })
	if listed == 0 || asked*4 > listed {
		t.Fatalf("ModelAPI allocated %d bytes, listing the protocols %d", asked, listed)
	}
}
