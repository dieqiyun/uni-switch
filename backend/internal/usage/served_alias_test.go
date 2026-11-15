package usage

import "testing"

// DeepSeek renamed its V4.1 Flash deepseek-flash (api-docs.deepseek.com
// /updates, 2026-09-10: "Change the model name to deepseek-flash to call
// the latest V4.1 Flash model"): a request sent under either name answered
// under the other is the model asked for, not another one swapped in
// (#4593, chowyuan1314). A dated spelling of either name is the same pair,
// as is another case or separator. Another DeepSeek model answered under
// the alias's name, or the alias answered under another model's name, is
// still a swap.
func TestSwappedVendorAlias(t *testing.T) {
	for _, c := range [][2]string{
		{"deepseek-v4.1-flash", "deepseek-flash"},
		{"deepseek-flash", "deepseek-v4.1-flash"},
		{"DeepSeek-V4.1-Flash", "deepseek_flash"},
		{"deepseek_v4_1_flash", "deepseek-flash"},
		{"deepseek-v4.1-flash", "us.deepseek.deepseek-flash"},
		{"deepseek-v4.1-flash-1017", "deepseek-flash"},
		{"deepseek-flash", "deepseek-v4.1-flash-1017"},
		{"top-api/deepseek-v4.1-flash", "deepseek-flash"},
	} {
		if Swapped(c[0], c[1]) {
			t.Errorf("%s answered as %s is the model asked for", c[0], c[1])
		}
	}
	for _, c := range [][2]string{
		{"deepseek-v4.1-pro", "deepseek-flash"},
		{"deepseek-v4-flash", "deepseek-flash"},
		{"deepseek-chat", "deepseek-flash"},
		{"deepseek-v4.1-flash", "deepseek-chat"},
		{"deepseek-v4.1-flash", "deepseek-reasoner"},
		{"deepseek-v4.1-flash", "deepseek-v4.1-flash-thinking"},
		{"gpt-5", "deepseek-flash"},
	} {
		if !Swapped(c[0], c[1]) {
			t.Errorf("%s answered as %s is another model", c[0], c[1])
		}
	}
}

// A row kept marked swapped before DeepSeek's alias was known is read back
// as not swapped; a real swap stays one.
func TestKeptRowVendorAliasNotSwapped(t *testing.T) {
	c := &rowChunk{}
	c.add(Row{Record: Record{Model: "deepseek-v4.1-flash", Served: "deepseek-flash"}, Swapped: true}, "", 0, false)
	c.add(Row{Record: Record{Model: "deepseek-v4.1-flash", Served: "deepseek-chat"}, Swapped: true}, "", 1, false)
	for _, chunk := range []*rowChunk{c, c.pack().unpack()} {
		if chunk.row(0).Swapped || !chunk.row(1).Swapped {
			t.Fatalf("alias %v, other model %v", chunk.row(0).Swapped, chunk.row(1).Swapped)
		}
	}
}
