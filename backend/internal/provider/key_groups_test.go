package provider

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

func TestAPIKeyGroupsHaveIndependentCatalogAndRoutes(t *testing.T) {
	prefillHome(t)
	lists := &relayLists{by: map[string][]string{
		"qa-basic": {"shared", "basic-only"},
		"qa-pro":   {"shared", "pro-only"},
		"qa-empty": {},
	}}
	srv := lists.serve(t)
	p := Provider{ID: "relay", Name: "蝶祈云API", Chat: srv.URL + "/v1", Key: "qa-basic", KeyName: "普通组", KeyGroups: true,
		Keys: []KeyAccount{{Key: "qa-pro", Name: "高级组"}, {Key: "qa-empty", Name: "空组"}}}
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := settings.Save(settings.Settings{PlainNames: true}); err != nil {
		t.Fatal(err)
	}
	es := Catalog()
	if len(es) != 4 {
		t.Fatalf("group catalog has %d entries, want 4: %+v", len(es), IDs())
	}
	labels := Labels(es)
	for i, e := range es {
		q, model, ok := Resolve(e.ID)
		if !ok || model != e.Model || !q.KeyGroup() {
			t.Fatalf("group selection does not resolve: %s", e.ID)
		}
		keys, unlisted, barred, _ := q.KeysFor(model, nil)
		if len(keys) != 1 || len(unlisted)+len(barred) != 0 {
			t.Fatalf("selection %s can reach other groups: %+v", e.ID, keys)
		}
		group := "普通组"
		if keys[0].Key == "qa-pro" {
			group = "高级组"
		}
		if labels[i] != e.Name+" · "+group+" · 蝶祈云API" {
			t.Errorf("group label %q", labels[i])
		}
		if strings.Contains(e.ID+labels[i], "qa-") {
			t.Fatal("credential appears in client catalog")
		}
	}
	for _, id := range []string{"relay/shared", "shared"} {
		q, model, ok := Resolve(id)
		if !ok || model != "shared" || q.KeyGroup() {
			t.Errorf("legacy selection lost ordinary routing: %s", id)
		}
	}
	proID := "relay/~key/" + KeyID("qa-pro") + "/shared"
	if err := RenameKey("relay", KeyID("qa-pro"), "专属组"); err != nil {
		t.Fatal(err)
	}
	if e, ok := EntryOf(proID); !ok || e.Label() != "shared · 专属组 · 蝶祈云API" {
		t.Fatalf("renamed group lost stable selection: %+v", e)
	}
	lists.set("qa-pro", "pro-only", "new-only")
	p2, _ := Find("relay")
	if _, _, err := p2.Refetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := Resolve(proID); ok {
		t.Fatal("removed group model still resolves")
	}
	if !slices.Contains(IDs(), "relay/~key/"+KeyID("qa-pro")+"/new-only") {
		t.Fatal("new group model missing after refresh")
	}
	if err := SetKeyOn("relay", KeyID("qa-pro"), false); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := Resolve("relay/~key/" + KeyID("qa-pro") + "/pro-only"); ok {
		t.Fatal("disabled key group still resolves")
	}
	if _, _, ok := Resolve("relay/~key/unknown/shared"); ok {
		t.Fatal("unknown key alias resolves as an upstream model")
	}
}
