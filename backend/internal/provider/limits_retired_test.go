package provider

import (
	"maps"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// A Max output or Compact at the user set on a model the provider has
// since stopped listing comes back from the editor with every Save (#3305,
// LoKi: opencode-zen-free/space-bunny-free=65536 refused each Save of Zen
// Free, even with no model picked). Sent back as it is, it is kept and the
// rest is saved; changed, or typed for a model never listed, it is still
// refused; taken out, it comes off.
func TestRetiredModelLimitsDontBlockTheEditor(t *testing.T) {
	limitsHome(t)
	if err := Save(Provider{ID: "relay", Name: "relay", Chat: "https://relay.example/v1", Key: "k", Models: []string{"sol", "luna"}}); err != nil {
		t.Fatal(err)
	}
	if err := SetModelOutput("relay/luna", 65536); err != nil {
		t.Fatal(err)
	}
	if err := SetModelCompacts("relay", map[string]int{"luna": 200000}); err != nil {
		t.Fatal(err)
	}
	// luna leaves the provider's list
	p, err := Find("relay")
	if err != nil {
		t.Fatal(err)
	}
	p.Models = []string{"sol"}
	if err := Save(*p); err != nil {
		t.Fatal(err)
	}
	if p, _ = Find("relay"); ServesModel(*p, "luna") {
		t.Fatal("luna is still served; the test needs it gone")
	}

	for _, c := range []struct {
		name string
		set  func(map[string]int) error
		of   func(string) map[string]int
		read func() map[string]int
	}{
		{"outputs", func(m map[string]int) error { return SetModelOutputs("relay", m) }, OutputsOf, func() map[string]int { return settings.Load().ModelOutputs }},
		{"compacts", func(m map[string]int) error { return SetModelCompacts("relay", m) }, CompactsOf, func() map[string]int { return settings.Load().ModelCompacts }},
	} {
		t.Run(c.name, func(t *testing.T) {
			back := c.of("relay") // what the editor is given and sends again
			old := back["luna"]
			if old == 0 {
				t.Fatalf("the editor isn't given luna's: %v", back)
			}
			back["sol"] = 32000
			if err := c.set(back); err != nil {
				t.Fatalf("a Save with luna's kept as it was: %v", err)
			}
			if got := c.read(); !maps.Equal(got, map[string]int{"relay/luna": old, "relay/sol": 32000}) {
				t.Errorf("saved %v", got)
			}
			if err := c.set(map[string]int{"luna": old + 1, "sol": 32000}); err == nil {
				t.Error("a changed value for a model relay no longer serves was accepted")
			}
			if err := c.set(map[string]int{"luna": old, "sol": 32000, "typo": 5}); err == nil {
				t.Error("a model relay never served was accepted")
			}
			if got := c.read(); !maps.Equal(got, map[string]int{"relay/luna": old, "relay/sol": 32000}) {
				t.Errorf("a refused save changed them: %v", got)
			}
			if err := c.set(map[string]int{"sol": 32000}); err != nil {
				t.Fatal(err)
			}
			if got := c.read(); !maps.Equal(got, map[string]int{"relay/sol": 32000}) {
				t.Errorf("luna's taken out: %v", got)
			}
		})
	}
}
