package usage

import "testing"

// A relay that sells a model under a name of its own, its own word in
// front of the model's, is answered with the model's own name: that is the
// model asked for, not another one swapped in (#1498, liuweifeng). Another
// model behind a relay's word is still a swap, and a bare word is never
// taken for the model.
func TestSwappedIgnoresARelaysRename(t *testing.T) {
	for _, c := range [][2]string{
		{"moonshot-kimi-k3", "kimi-k3"},
		{"relay-a/moonshot-kimi-k3", "moonshotai/kimi-k3"},
		{"claude-deepseek-v4.1-flash", "deepseek-v4.1-flash"},
		{"claude-deepseek-v4.1-flash", "deepseek-v4-1-flash"},
		{"vip-glm-5.3", "GLM-5.3"},
	} {
		if Swapped(c[0], c[1]) {
			t.Errorf("%s answered as %s is the same model", c[0], c[1])
		}
	}
	for _, c := range [][2]string{
		{"moonshot-kimi-k3", "kimi-k2"},
		{"claude-deepseek-v4.1-flash", "deepseek-v4-flash"},
		{"deepseek-v4.1-flash", "flash"},
		{"gpt-5-mini", "mini"},
		{"kimi-k3", "moonshot-kimi-k3"},
	} {
		if !Swapped(c[0], c[1]) {
			t.Errorf("%s answered as %s is another model", c[0], c[1])
		}
	}
}

// A row kept marked swapped before a relay's rename was the same model is
// read back as not swapped.
func TestKeptRowRelayRenamedNotSwapped(t *testing.T) {
	c := &rowChunk{}
	c.add(Row{Record: Record{Model: "moonshot-kimi-k3", Served: "kimi-k3"}, Swapped: true}, "", 0, false)
	c.add(Row{Record: Record{Model: "moonshot-kimi-k3", Served: "kimi-k2"}, Swapped: true}, "", 1, false)
	for _, chunk := range []*rowChunk{c, c.pack().unpack()} {
		if chunk.row(0).Swapped || !chunk.row(1).Swapped {
			t.Fatalf("renamed %v, other model %v", chunk.row(0).Swapped, chunk.row(1).Swapped)
		}
	}
}
