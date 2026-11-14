package provider

import "testing"

// A decision provider's list that names media types ("image/png") rather
// than models.dev's words says its model sees, as OpenCode Go's does
// (catalog.Kinds); one that lists text alone says it doesn't.
func TestListedFactsMediaTypesSee(t *testing.T) {
	var f listedFacts
	f.Modalities.Input = []string{"text", "image/jpeg", "image/png"}
	if m := f.model("m"); m.ImageInput == nil || !*m.ImageInput || !m.Images {
		t.Errorf("media types: %+v", m)
	}
	f = listedFacts{}
	f.Architecture.InputModalities = []string{"text"}
	if m := f.model("m"); m.ImageInput == nil || *m.ImageInput || m.Images {
		t.Errorf("text only: %+v", m)
	}
}
