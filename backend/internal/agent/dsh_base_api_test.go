package agent

import (
	"os"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// nb5p on Discord: a custom provider set to the Responses API, whose Chat
// URL was found beside it, had its models written to dsh's Chat route, so
// dsh asked them on Chat Completions. And with Provider in model names
// off, its GPT-6.1 Terra, which another provider serves too, was named
// "GPT-6.1 Terra · MyGateway" beside the group found for it while its
// GPT-6.1 Sol had no suffix. The models go on uni-switch-responses (dsh's
// pi-ai takes "openai-responses" for a route), both named alone.
func TestDshCustomResponsesProvider(t *testing.T) {
	home, dir, web := dshRouteHome(t)
	dshKeyFixture(dir)
	base := "http://127.0.0.1:1/v1"
	for _, p := range []provider.Provider{
		{ID: "mygateway", Name: "MyGateway", Key: "k", Chat: base, Responses: base, BaseAPI: "responses", Models: []string{"gpt-6.1-sol", "gpt-6.1-terra"}},
		{ID: "other", Name: "Other", Key: "k", Chat: "http://127.0.0.1:2/v1", Models: []string{"gpt-6.1-terra"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := catalog.SaveLive("mygateway", base, []catalog.Model{{ID: "gpt-6.1-sol", Name: "GPT-6.1 Sol"}, {ID: "gpt-6.1-terra", Name: "GPT-6.1 Terra"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("other", "http://127.0.0.1:2/v1", []catalog.Model{{ID: "gpt-6.1-terra", Name: "GPT-6.1 Terra"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetHiddenModels("dsh", []string{"other/gpt-6.1-terra"}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetPlainNames(true); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(web, []byte("# Your patch layer for this dsh profile.\n"), 0o644)
	a := dsh(home)
	if err := a.Field("model").Set("uni-switch/mygateway/gpt-6.1-sol"); err != nil {
		t.Fatal(err)
	}
	ps := dshPiProviders(t, web)
	b, _ := os.ReadFile(web)
	names := map[string]string{}
	for _, m := range ps["uni-switch-responses"].Models {
		names[m.ID] = m.Name
	}
	for id, want := range map[string]string{"mygateway/gpt-6.1-sol": "GPT-6.1 Sol", "mygateway/gpt-6.1-terra": "GPT-6.1 Terra"} {
		got, ok := names[id]
		if !ok {
			t.Errorf("%s isn't on uni-switch-responses", id)
		} else if got != want {
			t.Errorf("%s named %q, want %q", id, got, want)
		}
		if contains(dshRouteModelIDs(ps["uni-switch"]), id) {
			t.Errorf("%s is on the Chat route", id)
		}
	}
	if t.Failed() {
		t.Logf("%s", b)
	}
}
