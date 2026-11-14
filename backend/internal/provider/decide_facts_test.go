package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// A decision model's window and whether it takes images (ARNO on Discord):
// read from a list that says them (OpenRouter's context_length and
// architecture.input_modalities), else what its vendor's docs say — Jev
// 32k and text, Clef 65,536 and images, Bailian's 65,536 and text — so
// the Providers page and the Gateway list show them.
func TestDecideModelsKnowWindowAndImages(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"perplexity/pplx-decider-v1-27b","context_length":262144,"architecture":{"input_modalities":["text","image"]}},{"id":"liquid/d1","context_length":65536,"architecture":{"input_modalities":["text"]}},{"id":"typesafe/jev-1.13"}]}`))
	}))
	defer up.Close()
	p := Provider{ID: "openrouter-s1", Name: "OpenRouter", Key: "k", Decide: up.URL + "/api/v1", ModelsURL: up.URL + "/api/v1/models?output_modalities=decisions"}
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := map[string][2]any{
		"perplexity/pplx-decider-v1-27b": {262144, true},
		"liquid/d1":                      {65536, false},
		"typesafe/jev-1.13":              {32000, false}, // the list said nothing: Jev's docs
	}
	check := func(who string, ms []catalog.Model, want map[string][2]any) {
		t.Helper()
		seen := 0
		for _, m := range ms {
			w, ok := want[m.ID]
			if !ok {
				continue
			}
			seen++
			if ListedWindow(m) != w[0] || m.ImageInput == nil || *m.ImageInput != w[1] || m.Images != w[1] {
				t.Errorf("%s %s: window %d, images %v (%v), want %v", who, m.ID, ListedWindow(m), m.Images, m.ImageInput, w)
			}
		}
		if seen != len(want) {
			t.Errorf("%s: %d of %d models listed: %v", who, seen, len(want), ms)
		}
	}
	check("openrouter", p.Available(), want)

	// presets, before any list: Workers AI's Jev and Clefs, Bailian's
	cf, err := FromPreset("cloudflare-jev")
	if err != nil {
		t.Fatal(err)
	}
	cf.Decide = "https://api.cloudflare.com/client/v4/accounts/acc7/ai/run"
	check("cloudflare", cf.Available(), map[string][2]any{CloudflareJev: {32000, false}, CloudflareClefModel: {65536, true}, CloudflareClefFlash: {65536, true}})
	bl := Provider{ID: "bailian-s1", Decide: "https://ws-1.cn-beijing.maas.aliyuncs.com/api/v1"}
	check("bailian", bl.Available(), map[string][2]any{BailianDecision: {65536, false}})
	ts := Provider{ID: "typesafe", Decide: "https://api.typesafe.ai/v1"}
	check("typesafe", ts.Available(), map[string][2]any{JevLatest: {32000, false}, "jev-preview": {32000, false}})

	// a gateway's Jev among its chat models has them too; its chat models
	// are left as they are
	if err := catalog.SaveLive("zen", "https://opencode.ai/zen/v1", []catalog.Model{{ID: "jev-1.13"}, {ID: "chat-x", Context: 1000}}); err != nil {
		t.Fatal(err)
	}
	zen := Provider{ID: "zen", Chat: "https://opencode.ai/zen/v1", Decide: "https://opencode.ai/zen/v1"}
	check("zen", zen.Available(), map[string][2]any{"jev-1.13": {32000, false}})
	for _, m := range zen.Available() {
		if m.ID == "chat-x" && (m.Context != 1000 || m.ImageInput != nil) {
			t.Errorf("a chat model changed: %+v", m)
		}
	}
}
