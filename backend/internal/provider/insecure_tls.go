package provider

// TakesInsecureTLS is whether the provider may have its certificate left
// unchecked (InsecureTLS, #2383): only one the user added with a base URL
// of their own. A subscription's or a plugin's account, a preset's (whose
// vendor serves a certificate that checks) and a built-in's never do,
// whatever providers.json says.
func (p Provider) TakesInsecureTLS() bool {
	return p.Account == nil && p.Preset == "" && !p.IsVertex() && hasEndpoint(p) && !subscriptionID(p.ID)
}

// UncheckedHosts are the base URLs whose host's certificate is taken as it
// is when the provider has InsecureTLS: its own endpoints, never a models,
// balance or image URL elsewhere.
func (p Provider) UncheckedHosts() []string {
	var out []string
	for _, u := range []string{p.Chat, p.Responses, p.Anthropic, p.Gemini, p.Decide} {
		if u != "" {
			out = append(out, u)
		}
	}
	return out
}
