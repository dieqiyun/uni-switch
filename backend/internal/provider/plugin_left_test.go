package provider

import (
	"slices"
	"testing"
)

// writeProviders puts ps in providers.json as the user's file has them.
func writeProviders(t *testing.T, ps []Provider) {
	t.Helper()
	if err := store(file{Providers: ps}); err != nil {
		t.Fatal(err)
	}
}

func storedIDs(t *testing.T) []string {
	t.Helper()
	ps, err := Stored()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, p := range ps {
		ids = append(ids, p.ID)
	}
	slices.Sort(ids)
	return ids
}

func mustStored(t *testing.T) []Provider {
	t.Helper()
	ps, err := Stored()
	if err != nil {
		t.Fatal(err)
	}
	return ps
}
