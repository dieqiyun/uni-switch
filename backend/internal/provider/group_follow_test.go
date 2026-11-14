package provider

import (
	"testing"
)

func saveKeyProvider(t *testing.T, id string, models ...string) {
	t.Helper()
	if err := Save(Provider{ID: id, Name: id, Key: "k", Chat: "http://127.0.0.1:1/v1", Models: models}); err != nil {
		t.Fatal(err)
	}
}
