package provider

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// liveFree writes the list a vendor's /models answered, where
// catalog.Live reads it: what Available goes by, and so FreeModel.
func liveFree(t *testing.T, id, json string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(catalog.LivePath(id)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.LivePath(id), []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
}

// FreeModel is what Free first goes by: a model its vendor lists at no cost
// is free, one the vendor prices is not, and a model the list doesn't name
// is not either — nothing says it costs nothing, so Free first puts it with
// the paid ones rather than inventing a saving.
func TestFreeModelGoesByTheVendorsOwnList(t *testing.T) {
	prefsHome(t)
	p := Provider{ID: "fv", Name: "FV", Key: "sk-fv", Chat: "http://127.0.0.1:9/v1"}
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	liveFree(t, "fv", `{"models":[
		{"id":"free-one","free":true},
		{"id":"paid-one","cost":{"input":3,"output":15}},
		{"id":"zero-priced"}
	]}`)
	got, err := Find("fv")
	if err != nil {
		t.Fatal(err)
	}
	if !got.FreeModel("free-one") {
		t.Error("a model its vendor lists free isn't free")
	}
	if got.FreeModel("paid-one") {
		t.Error("a model its vendor prices as paid is free")
	}
	if got.FreeModel("nobody-lists-this") {
		t.Error("a model the list doesn't name is free")
	}
}

// The vendor's own word is what decides it, however many models it so
// marks: a pool where some are free and some are not puts only the marked
// ones first, and one where none is marked behaves as In order.
func TestFreeModelIsOnlyWhereTheVendorSaysSo(t *testing.T) {
	prefsHome(t)
	p := Provider{ID: "mv", Name: "MV", Key: "sk-mv", Chat: "http://127.0.0.1:9/v1"}
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	liveFree(t, "mv", `{"models":[{"id":"marked","free":true},{"id":"plain"},{"id":"also-marked","free":true}]}`)
	got, err := Find("mv")
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"marked", "also-marked"} {
		if !got.FreeModel(model) {
			t.Errorf("%s isn't free", model)
		}
	}
	if got.FreeModel("plain") {
		t.Error("plain is free")
	}
}
