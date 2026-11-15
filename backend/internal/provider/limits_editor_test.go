package provider

import (
	"maps"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// A provider's editor sets its reply limits as a whole, as it sets its
// windows (ARNO on Discord: a model's maxTokens was wrong and only the
// window could be set there): those it leaves out come off, another
// provider's stay, a model it doesn't serve is refused with nothing kept,
// and a save that changes nothing tells the agents nothing.
func TestModelOutputsSetFromTheEditor(t *testing.T) {
	limitsHome(t)
	for _, id := range []string{"relay", "other"} {
		if err := Save(Provider{ID: id, Name: id, Chat: "https://relay.example/v1", Key: "k", Models: []string{"sol", "luna"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := SetModelOutput("relay/luna", 1000); err != nil {
		t.Fatal(err)
	}
	if err := SetModelOutput("other/sol", 2000); err != nil {
		t.Fatal(err)
	}
	if got := OutputsOf("relay"); !maps.Equal(got, map[string]int{"luna": 1000}) {
		t.Fatalf("OutputsOf: %v", got)
	}
	told := 0
	catalog.Changed = func() { told++ }
	t.Cleanup(func() { catalog.Changed = nil })

	if err := SetModelOutputs("relay", map[string]int{"*": 32000, "sol": 128000}); err != nil {
		t.Fatal(err)
	}
	if told != 1 {
		t.Errorf("told the agents %d times; want once", told)
	}
	want := map[string]int{"relay/*": 32000, "relay/sol": 128000, "other/sol": 2000}
	if got := settings.Load().ModelOutputs; !maps.Equal(got, want) {
		t.Errorf("outputs %v; want %v", got, want)
	}
	if e, _ := EntryOf("relay/sol"); e.Output != 128000 {
		t.Errorf("relay/sol output %d", e.Output)
	}
	if e, _ := EntryOf("relay/luna"); e.Output != 32000 {
		t.Errorf("relay/luna output %d; want the provider's 32000", e.Output)
	}

	told = 0
	if err := SetModelOutputs("relay", map[string]int{"*": 32000, "sol": 128000}); err != nil || told != 0 {
		t.Errorf("the same again: %v, told %d", err, told)
	}
	if err := SetModelOutputs("relay", map[string]int{"typo": 5}); err == nil {
		t.Error("a model relay doesn't serve was accepted")
	}
	if got := settings.Load().ModelOutputs; !maps.Equal(got, want) {
		t.Errorf("a refused save changed them: %v", got)
	}
	if err := SetModelOutputs("relay", map[string]int{}); err != nil {
		t.Fatal(err)
	}
	if got := settings.Load().ModelOutputs; !maps.Equal(got, map[string]int{"other/sol": 2000}) {
		t.Errorf("emptied: %v", got)
	}
}
