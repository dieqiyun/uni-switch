package provider

import (
	"bytes"
	"os"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// Save pressed on a provider's editor left as it was writes nothing and
// hands the agents nothing: each hand-over re-wrote every agent's config,
// seconds with many agents set up, and the DeepSeek editor's Save hung on
// it (#1962, witt-bit). A save that changes something still tells them.
func TestSaveUnchangedTellsNoAgent(t *testing.T) {
	limitsHome(t)
	in := Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "sk-1", Models: []string{"deepseek-v4-flash", "deepseek-v4-pro"}}
	if err := Save(in); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	told := 0
	catalog.Changed = func() { told++ }
	t.Cleanup(func() { catalog.Changed = nil })

	if err := Save(in); err != nil {
		t.Fatal(err)
	}
	if told != 0 {
		t.Errorf("an unchanged save told the agents %d times; want none", told)
	}
	after, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("an unchanged save rewrote providers.json:\n%s\nwas\n%s", after, before)
	}

	in.Key = "sk-2"
	if err := Save(in); err != nil {
		t.Fatal(err)
	}
	if told != 1 {
		t.Errorf("a new key told the agents %d times; want once", told)
	}
	if p, err := Find("deepseek"); err != nil || p.Key != "sk-2" {
		t.Errorf("the new key wasn't saved: %+v, %v", p, err)
	}
}
