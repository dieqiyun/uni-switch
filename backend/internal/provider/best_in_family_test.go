package provider

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// Best in family (Steven on Discord): with nothing picked, only the newest
// model of each family the vendor lists is served, and one it lists later
// takes the old one's place by itself. A request naming an older one is
// still served, as one naming a model left unpicked is; a pick or Select
// none clears it, and a providers.json without it reads as before.
func TestBestInFamilyExposed(t *testing.T) {
	azureHome(t)
	ids := func(ms []catalog.Model) []string {
		var out []string
		for _, m := range ms {
			out = append(out, m.ID)
		}
		return out
	}
	list := func(ids ...string) {
		t.Helper()
		var ms []catalog.Model
		for _, id := range ids {
			ms = append(ms, catalog.Model{ID: id})
		}
		if err := catalog.SaveLive("fam", "https://fam.example.invalid/v1", ms); err != nil {
			t.Fatal(err)
		}
	}
	find := func() Provider {
		t.Helper()
		p, err := Find("fam")
		if err != nil {
			t.Fatal(err)
		}
		return *p
	}
	p := Provider{ID: "fam", Name: "Fam", Key: "sk-x", Chat: "https://fam.example.invalid/v1"}
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	list("claude-opus-5-5", "claude-sonnet-5-5", "claude-opus-4-8", "claude-haiku-5-5", "claude-sonnet-4-5-20250929", "claude-haiku-4-5")
	all := []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-opus-4-8", "claude-haiku-5-5", "claude-sonnet-4-5-20250929", "claude-haiku-4-5"}

	// a file saved before it: the vendor's list, as ever
	if got := find(); got.BestInFamily || !slices.Equal(ids(got.Exposed()), all) {
		t.Fatalf("before: best %v exposed %v", got.BestInFamily, ids(got.Exposed()))
	}
	raw, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	var file map[string]any
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if containsKey(file, "bestInFamily") {
		t.Fatalf("a provider saved without it wrote bestInFamily: %s", raw)
	}

	p.BestInFamily = true
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	want := []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"}
	if got := find(); !got.BestInFamily || !slices.Equal(ids(got.Exposed()), want) {
		t.Fatalf("best: %v exposed %v, want %v", got.BestInFamily, ids(got.Exposed()), want)
	}
	if got := ids(find().NewestInFamily(find().Available())); !slices.Equal(got, want) {
		t.Errorf("NewestInFamily = %v, want %v", got, want)
	}
	// an older one named by its provider/model id is still served, as an
	// unpicked one is under Picked only
	if q, m, ok := Resolve("fam/claude-opus-4-8"); !ok || q.ID != "fam" || m != "claude-opus-4-8" {
		t.Errorf("fam/claude-opus-4-8: %v %q %v", q.ID, m, ok)
	}
	// Opus 5.6 listed later takes Opus 5.5's place, with nothing saved
	list("claude-opus-5-6", "claude-opus-5-5", "claude-sonnet-5-5", "claude-opus-4-8", "claude-haiku-5-5")
	if got := ids(find().Exposed()); !slices.Equal(got, []string{"claude-opus-5-6", "claude-sonnet-5-5", "claude-haiku-5-5"}) {
		t.Errorf("after Opus 5.6 is listed: %v", got)
	}
	// a save of something else keeps it
	q := find()
	q.Name = "Fam renamed"
	if err := Save(q); err != nil {
		t.Fatal(err)
	}
	if !find().BestInFamily {
		t.Error("a rename dropped Best in family")
	}
	// a pick clears it
	if err := SetModels("fam", []string{"claude-opus-4-8"}); err != nil {
		t.Fatal(err)
	}
	if got := find(); got.BestInFamily || !slices.Equal(ids(got.Exposed()), []string{"claude-opus-4-8"}) {
		t.Errorf("a pick: best %v exposed %v", got.BestInFamily, ids(got.Exposed()))
	}
	// and so does Select none
	q = find()
	q.Models, q.NoModels, q.BestInFamily = nil, true, true
	if err := Save(q); err != nil {
		t.Fatal(err)
	}
	if got := find(); got.BestInFamily || !got.ServesNone() {
		t.Errorf("Select none: best %v none %v", got.BestInFamily, got.ServesNone())
	}
}

func containsKey(v any, key string) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if k == key || containsKey(e, key) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if containsKey(e, key) {
				return true
			}
		}
	}
	return false
}
