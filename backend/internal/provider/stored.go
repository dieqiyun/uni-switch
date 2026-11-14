package provider

// Stored reads the saved providers, including their keys, for gateway state handling.
func Stored() ([]Provider, error) {
	f, err := read()
	return f.Providers, err
}

// OnlyPicks reports whether a saved row has subscription choices but no API endpoint.
func OnlyPicks(p Provider) bool { return !hasEndpoint(normalize(p)) }
