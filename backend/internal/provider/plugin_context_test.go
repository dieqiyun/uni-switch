package provider

import (
	"testing"
)

func pluginWindow(t *testing.T, id string) int {
	t.Helper()
	for _, e := range Served() {
		if e.ID == id {
			return e.Context
		}
	}
	t.Fatalf("no %s in %v", id, IDs())
	return 0
}
