package provider

import "slices"

// endpointOf is the gateway path a protocol is asked at.
var endpointOf = map[Protocol]string{Chat: "/v1/chat/completions", Responses: "/v1/responses", Anthropic: "/v1/messages"}

// NativeEndpoints are the paths a request for the model is relayed on to
// its provider as it is (/v1/models' and an agent plugin's connect's
// "native_endpoints"): the APIs the provider serves it on. A routing
// group's are GroupEndpoints, which need its members; none here. None
// either for a model every request to is translated anyway (a
// subscription served through its agent's own API).
func NativeEndpoints(e Entry) []string {
	{
	}
	return endpointsOf(relayed(e.Provider, e.Model))
}

// relayed are the APIs p takes a request for model on as it is, in the
// order p speaks them: none for a model every request to is translated
// anyway.
func relayed(p Provider, model string) []Protocol {
	if p.Native(model) == "" {
		return nil
	}
	apis := p.APIs(model)
	var out []Protocol
	for _, pr := range p.Speaks() {
		if slices.Contains(Protocols, pr) && (apis == nil || slices.Contains(apis, pr)) {
			out = append(out, pr)
		}
	}
	return out
}

func endpointsOf(prs []Protocol) []string {
	var out []string
	for _, pr := range prs {
		out = append(out, endpointOf[pr])
	}
	return out
}
