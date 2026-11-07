package gateway

// estimate follows Magpie gateway.estimate: content/tool byte sizes divided
// by four. It is deliberately approximate, not a vendor tokenizer or probe.
func estimate(r *Request) int {
	n := len(r.System)
	for _, turn := range r.Turns {
		for _, p := range turn.Parts {
			n += len(p.Text) + len(p.Name)
			if p.Args != nil {
				n += len(rawArguments(p.Args))
			}
		}
	}
	for _, tool := range r.Tools {
		n += len(tool.Name) + len(tool.Description)
		if tool.Schema != nil {
			n += len(encode(tool.Schema))
		}
	}
	return n / 4
}
