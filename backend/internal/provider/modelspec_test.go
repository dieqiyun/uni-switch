package provider

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// specHome is a sandbox with a custom relay whose models no list knows
// (Dani Ariyanto on Discord: kenari/*:free).
func specHome(t *testing.T) {
	t.Helper()
	limitsHome(t)
	if err := Save(Provider{ID: "kenari", Name: "Kenari", Chat: "http://127.0.0.1:1/v1", Key: "k",
		Models: []string{"glm-5:free", "qwen-x:free"}}); err != nil {
		t.Fatal(err)
	}
}

// specFiles are the bytes of the two files a spec is kept in.
func specFiles(t *testing.T) string {
	t.Helper()
	var out string
	for _, f := range []string{"settings.json", "providers.json"} {
		b, _ := os.ReadFile(filepath.Join(settings.Dir(), f))
		out += f + ":" + string(b) + "\n"
	}
	return out
}

// A custom provider's model with nothing known takes a whole spec at once,
// the catalog entry every reader goes by carries it, `uni-switch model specs`
// lists it, and a reset leaves the model as it was before, unknown on
// every part. The agents are told once for a change that writes both files.
func TestModelSpecSetListReset(t *testing.T) {
	specHome(t)
	e, ok := EntryOf("kenari/glm-5:free")
	if !ok {
		t.Fatal("kenari/glm-5:free is not in the catalog")
	}
	if e.Context != 0 || e.Output != 0 || e.Images || e.ImageInput != nil || len(e.Efforts) != 0 {
		t.Fatalf("before: a model nothing knows has %+v", e)
	}
	told := 0
	catalog.Changed = func() { told++ }
	t.Cleanup(func() { catalog.Changed = nil })

	if err := SetModelSpec("kenari/glm-5:free", SpecChange{
		Context: new(131072), Output: new(16384), Vision: new("on"), Reasoning: new("low-high"),
	}); err != nil {
		t.Fatal(err)
	}
	if told != 1 {
		t.Errorf("the agents were told %d times; want once", told)
	}
	e, _ = EntryOf("kenari/glm-5:free")
	if e.Context != 131072 || e.Output != 16384 || !e.Images || e.ImageInput == nil || !*e.ImageInput {
		t.Errorf("entry after set: context %d output %d images %v", e.Context, e.Output, e.Images)
	}
	if !slices.Equal(e.Efforts, []string{"low", "medium", "high"}) || !e.Reasoning {
		t.Errorf("levels after set: %v reasoning %v", e.Efforts, e.Reasoning)
	}
	// the other model of the provider is left as it was
	if o, _ := EntryOf("kenari/qwen-x:free"); o.Context != 0 || o.Images || len(o.Efforts) != 0 {
		t.Errorf("the model given nothing changed: %+v", o)
	}
	rows := ModelSpecs()
	if len(rows) != 1 || rows[0] != (SpecRow{Key: "kenari/glm-5:free", Context: "131072", Output: "16384", Vision: "on", Reasoning: "low,medium,high"}) {
		t.Errorf("specs: %+v", rows)
	}
	sp, ok := SpecOf("kenari/glm-5:free")
	if !ok || sp.Context.From != "model" || sp.Vision.From != "model" || sp.Reasoning.Value != "low,medium,high" {
		t.Errorf("spec: %+v", sp)
	}

	// one part back to auto leaves the others
	if err := SetModelSpec("kenari/glm-5:free", SpecChange{Vision: new("auto")}); err != nil {
		t.Fatal(err)
	}
	if e, _ = EntryOf("kenari/glm-5:free"); e.Images || e.ImageInput != nil || e.Context != 131072 {
		t.Errorf("vision auto: images %v %v context %d", e.Images, e.ImageInput, e.Context)
	}

	gone, err := ResetModelSpec("kenari/glm-5:free")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(gone, []string{"context", "output", "reasoning"}) {
		t.Errorf("reset took away %v", gone)
	}
	e, _ = EntryOf("kenari/glm-5:free")
	if e.Context != 0 || e.Output != 0 || e.Images || len(e.Efforts) != 0 || e.Reasoning {
		t.Errorf("after reset: %+v", e)
	}
	if rows := ModelSpecs(); len(rows) != 0 {
		t.Errorf("specs after reset: %+v", rows)
	}
	if gone, err := ResetModelSpec("kenari/glm-5:free"); err != nil || len(gone) != 0 {
		t.Errorf("a second reset: %v %v", gone, err)
	}
}

// A bad part — a level the model hasn't, a level that is no level, a range
// that runs backwards, a model the provider doesn't serve, vision for a
// whole provider, a model from any provider — is refused, and nothing of
// the change is written, not even its good parts.
func TestModelSpecRefusalWritesNothing(t *testing.T) {
	specHome(t)
	if err := SetModelSpec("kenari/glm-5:free", SpecChange{Context: new(65536)}); err != nil {
		t.Fatal(err)
	}
	before := specFiles(t)
	for name, c := range map[string]struct {
		ref string
		c   SpecChange
	}{
		"not a level":      {"kenari/glm-5:free", SpecChange{Context: new(1 << 20), Reasoning: new("turbo")}},
		"backwards range":  {"kenari/glm-5:free", SpecChange{Output: new(4096), Reasoning: new("high-low")}},
		"bad vision":       {"kenari/glm-5:free", SpecChange{Context: new(1 << 20), Vision: new("maybe")}},
		"unserved model":   {"kenari/nope", SpecChange{Context: new(1 << 20)}},
		"provider vision":  {"kenari/*", SpecChange{Context: new(1 << 20), Vision: new("on")}},
		"provider levels":  {"kenari/*", SpecChange{Reasoning: new("low")}},
		"any provider":     {"*/glm-5:free", SpecChange{Context: new(1 << 20)}},
		"negative":         {"kenari/glm-5:free", SpecChange{Output: new(-1), Vision: new("on")}},
		"nothing said":     {"kenari/glm-5:free", SpecChange{}},
		"no such provider": {"nobody/glm-5:free", SpecChange{Context: new(1 << 20)}},
	} {
		if err := SetModelSpec(c.ref, c.c); err == nil {
			t.Errorf("%s: %s %+v was taken", name, c.ref, c.c)
		}
		if got := specFiles(t); got != before {
			t.Fatalf("%s: a refused spec wrote:\nbefore %s\nafter  %s", name, before, got)
		}
	}
}

// A model's own window and reply limit win over the ones given every model
// of its provider ("kenari/*"), which a model with none of its own takes;
// a reset of the model leaves it the provider's.
func TestModelSpecProviderWildcard(t *testing.T) {
	specHome(t)
	if err := SetModelSpec("kenari/*", SpecChange{Context: new(65536), Output: new(8192)}); err != nil {
		t.Fatal(err)
	}
	if err := SetModelSpec("kenari/glm-5:free", SpecChange{Context: new(131072), Output: new(16384)}); err != nil {
		t.Fatal(err)
	}
	if e, _ := EntryOf("kenari/glm-5:free"); e.Context != 131072 || e.Output != 16384 {
		t.Errorf("the model's own: %d %d", e.Context, e.Output)
	}
	if e, _ := EntryOf("kenari/qwen-x:free"); e.Context != 65536 || e.Output != 8192 {
		t.Errorf("the provider's: %d %d", e.Context, e.Output)
	}
	if sp, _ := SpecOf("kenari/qwen-x:free"); sp.Context.From != "provider" || sp.Output.From != "provider" {
		t.Errorf("spec from: %+v", sp)
	}
	if _, err := ResetModelSpec("kenari/glm-5:free"); err != nil {
		t.Fatal(err)
	}
	if e, _ := EntryOf("kenari/glm-5:free"); e.Context != 65536 || e.Output != 8192 {
		t.Errorf("after the model's reset: %d %d", e.Context, e.Output)
	}
	if gone, err := ResetModelSpec("kenari/*"); err != nil || !slices.Equal(gone, []string{"context", "output"}) {
		t.Errorf("provider reset: %v %v", gone, err)
	}
	if e, _ := EntryOf("kenari/qwen-x:free"); e.Context != 0 || e.Output != 0 {
		t.Errorf("after the provider's reset: %d %d", e.Context, e.Output)
	}
}
