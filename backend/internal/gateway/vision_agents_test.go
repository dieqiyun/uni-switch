package gateway

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/codexcat"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A text-only model is offered to agents as taking images while uni-switch
// describes images to it (Discord, Fate: Codex turned an image away for a
// text model although Settings › Vision was set), and as text-only again
// with Vision off.
func TestDescribedModelsTakeImagesInAgentsLists(t *testing.T) {
	eyed(t)
	images := func() map[string]bool {
		out := map[string]bool{}
		for _, m := range provider.CodexListed() {
			out[m.ID] = m.Images
		}
		return out
	}
	modalities := func(id string) []string {
		b, _ := json.Marshal(codexcat.Entries(provider.CodexListed(), 0))
		var es []struct {
			Slug string   `json:"slug"`
			In   []string `json:"input_modalities"`
		}
		_ = json.Unmarshal(b, &es)
		for _, e := range es {
			if e.Slug == id {
				return e.In
			}
		}
		return nil
	}
	if got := images(); !got["probe/text"] || !got["probe/eye"] {
		t.Fatalf("with Vision on, Codex is told: %v", got)
	}
	if in := modalities("probe/text"); !slices.Contains(in, "image") {
		t.Fatalf("probe/text's input_modalities = %v", in)
	}
	on := provider.CodexListTag()
	noVision(t)
	if got := images(); got["probe/text"] || !got["probe/eye"] {
		t.Fatalf("with Vision off, Codex is told: %v", got)
	}
	if provider.CodexListTag() == on {
		t.Fatal("Codex's list tag didn't change with Vision")
	}
}
