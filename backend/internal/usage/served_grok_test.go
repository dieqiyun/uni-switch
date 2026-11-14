package usage

import "testing"

// xAI answers a request for grok-4.7 as grok-4.7-build (#1455, cybershape's
// requests log): its name for the build serving the model, not another
// model. The fast variant, another version, another Grok model and another
// vendor's "-build" are still swaps.
func TestSwappedGrokServingName(t *testing.T) {
	for _, c := range [][2]string{
		{"grok-4.7", "grok-4.7-build"},
		{"grok/grok-4.7", "grok-4.7-build"},
		{"Grok-4.7", "grok-4.7-build"},
		{"grok-4.6", "grok-4.6-build"},
		{"grok-4.7-build-fast", "grok-4.7-build-fast"},
		{"grok-4.7-fast", "grok-4.7-build-fast"},
	} {
		if Swapped(c[0], c[1]) {
			t.Errorf("%s answered as %s is the model asked for", c[0], c[1])
		}
	}
	for _, c := range [][2]string{
		{"grok-4.7", "grok-4.7-build-fast"},
		{"grok-4.7-build-fast", "grok-4.7-build"},
		{"grok-4.7-fast", "grok-4.7-build"},
		{"grok-4.7", "grok-4.6-build"},
		{"grok-4.7", "grok-4.7-mini"},
		{"grok-4.7", "grok-4.7-builder"},
		{"gpt-5", "gpt-5-build"},
		{"claude-sonnet-4-5", "claude-sonnet-4-5-build"},
	} {
		if !Swapped(c[0], c[1]) {
			t.Errorf("%s answered as %s is another model", c[0], c[1])
		}
	}
}

// A model offered at a window beside its default (grok-4.7@500k) is
// answered under its plain name: the same model, not a swap (#1645,
// ggbdpq). Another version or variant at that window is still one.
func TestSwappedGrokWindowID(t *testing.T) {
	for _, c := range [][2]string{
		{"grok-4.7@500k", "grok-4.7"},
		{"grok-4.7@500k", "grok-4.7-build"},
		{"grok-4.6@256k", "grok-4.6"},
		{"x-ai/grok-4.7@500k", "grok-4.7"},
		{"claude-opus-5-5@300k", "claude-opus-5-5"},
	} {
		if Swapped(c[0], c[1]) {
			t.Errorf("%s answered as %s is the same model at a window", c[0], c[1])
		}
	}
	for _, c := range [][2]string{
		{"grok-4.7@500k", "grok-4.6"},
		{"grok-4.7@500k", "grok-4.7-mini"},
		{"grok-4.7@500k", "grok-4.7-build-fast"},
		{"grok-4.7@500k", "grok-4.6-build"},
	} {
		if !Swapped(c[0], c[1]) {
			t.Errorf("%s answered as %s is another model", c[0], c[1])
		}
	}
}

// A row kept marked swapped before is read back as not swapped when xAI
// named the build serving it; a real swap stays one.
func TestKeptRowGrokServingNameNotSwapped(t *testing.T) {
	c := &rowChunk{}
	c.add(Row{Record: Record{Model: "grok-4.7", Served: "grok-4.7-build"}, Swapped: true}, "", 0, false)
	c.add(Row{Record: Record{Model: "grok-4.7", Served: "grok-4.7-build-fast"}, Swapped: true}, "", 1, false)
	for _, chunk := range []*rowChunk{c, c.pack().unpack()} {
		if chunk.row(0).Swapped || !chunk.row(1).Swapped {
			t.Fatalf("serving name %v, fast variant %v", chunk.row(0).Swapped, chunk.row(1).Swapped)
		}
	}
}
