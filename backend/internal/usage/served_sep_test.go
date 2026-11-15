package usage

import "testing"

// A vendor that writes a version's dots as hyphens or underscores answered
// with the model asked for (vincentzhang on Discord: Volcengine Ark's
// deepseek-v4.1-flash answered as deepseek-v4-1-flash, marked another
// model); another model is still one.
func TestSwappedIgnoresSeparators(t *testing.T) {
	for _, c := range [][2]string{
		{"volcengine/deepseek-v4.1-flash", "deepseek-v4-1-flash"},
		{"deepseek-v4.1-flash", "deepseek_v4_1_flash"},
		{"glm-4.6", "GLM-4-6"},
		{"gpt-4.1", "gpt-4-1-2025-04-14"},
		{"doubao-seed-1.6", "doubao-seed-1-6-250615"},
	} {
		if Swapped(c[0], c[1]) {
			t.Errorf("%s answered as %s is the same model", c[0], c[1])
		}
	}
	for _, c := range [][2]string{
		{"deepseek-v4.1-flash", "deepseek-v4-flash"},
		{"deepseek-v4.1-flash", "deepseek-v4.1-pro"},
		{"glm-4.6", "glm-4.5"},
		{"claude-opus-5-5", "claude-sonnet-5-5"},
	} {
		if !Swapped(c[0], c[1]) {
			t.Errorf("%s answered as %s is another model", c[0], c[1])
		}
	}
}

// A row kept marked swapped before is read back as not swapped when its
// model was only spelled otherwise; a real swap stays one.
func TestKeptRowSameSpelledNotSwapped(t *testing.T) {
	c := &rowChunk{}
	c.add(Row{Record: Record{Model: "deepseek-v4.1-flash", Served: "deepseek-v4-1-flash"}, Swapped: true}, "", 0, false)
	c.add(Row{Record: Record{Model: "deepseek-v4.1-flash", Served: "deepseek-v4-flash"}, Swapped: true}, "", 1, false)
	for _, chunk := range []*rowChunk{c, c.pack().unpack()} {
		if chunk.row(0).Swapped || !chunk.row(1).Swapped {
			t.Fatalf("same-spelled %v, other model %v", chunk.row(0).Swapped, chunk.row(1).Swapped)
		}
	}
}
