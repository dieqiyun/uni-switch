package usage

import "testing"

// ClinePass answers a cline-pass/ model under its virtual model config's
// name, the version's dot written "p" (cline-pass/deepseek-v4.1-flash-1017
// as vmc/deepseek-v4p1-flash-1017, hy125245 on X and zRain on Discord):
// the model asked for, whichever upstream served it. Another model under
// a vmc/ name, and a vmc/ name for a model not asked of ClinePass, are
// still swaps.
func TestSwappedClineServingName(t *testing.T) {
	for _, c := range [][2]string{
		{"cline-pass/deepseek-v4.1-flash-1017", "vmc/deepseek-v4p1-flash-1017"},
		{"cline/cline-pass/deepseek-v4.1-flash-1017", "vmc/deepseek-v4p1-flash-1017"},
		{"cline-pass/deepseek-v4.1-flash", "vmc/deepseek-v4p1-flash"},
		{"cline-pass/deepseek-v4.1-flash", "VMC/DeepSeek-V4p1-Flash"},
		{"cline-pass/glm-5.3", "vmc/glm-5p3"},
		{"cline-pass/deepseek-v4-pro", "vmc/deepseek-v4-pro"},
	} {
		if Swapped(c[0], c[1]) {
			t.Errorf("%s answered as %s is the model asked for", c[0], c[1])
		}
	}
	for _, c := range [][2]string{
		{"cline-pass/deepseek-v4-pro", "vmc/deepseek-v4p1-flash"},
		{"cline-pass/deepseek-v4.1-flash", "vmc/deepseek-v4p1-pro"},
		{"cline-pass/deepseek-v4.1-flash", "vmc/deepseek-v4p2-flash"},
		{"cline-pass/glm-5.3", "vmc/glm-5p3-flash"},
		{"deepseek/deepseek-v4.1-flash", "vmc/deepseek-v4p1-flash"},
		{"cline-pass/deepseek-v4.1-flash", "other/deepseek-v4p1-flash"},
		{"cline-pass/deepseek-v4.1-flash-1017", "vmc/deepseek-v4p1-flash-1017-contributor-fallbacks"},
		{"cline-pass/deepseek-v4.1-flash", "vmc/deepseek-v4p1-flash-contributor-fallbacks"},
		{"cline-pass/deepseek-v4.1-flash-1017", "vmc/deepseek-v4p1-flash-1017-contributor"},
		{"cline-pass/deepseek-v4.1-flash-1017", "vmc/deepseek-v4p1-flash-1017-fallbacks"},
	} {
		if !Swapped(c[0], c[1]) {
			t.Errorf("%s answered as %s is another model", c[0], c[1])
		}
	}
}

// A row kept marked swapped before is read back as not swapped when
// ClinePass named its vmc/ config for the model; a real swap stays one.
func TestKeptRowClineServingNameNotSwapped(t *testing.T) {
	c := &rowChunk{}
	c.add(Row{Record: Record{Model: "cline-pass/deepseek-v4.1-flash-1017", Served: "vmc/deepseek-v4p1-flash-1017"}, Swapped: true}, "", 0, false)
	c.add(Row{Record: Record{Model: "cline-pass/deepseek-v4-pro", Served: "vmc/deepseek-v4p1-flash"}, Swapped: true}, "", 1, false)
	for _, chunk := range []*rowChunk{c, c.pack().unpack()} {
		if chunk.row(0).Swapped || !chunk.row(1).Swapped {
			t.Fatalf("serving name %v, another model %v", chunk.row(0).Swapped, chunk.row(1).Swapped)
		}
	}
}
