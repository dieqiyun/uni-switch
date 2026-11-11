package gateway

import (
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// routerUp is a router key whose vendor lists a small model that sees,
// anthropic/claude-haiku-5.5, which the user left unpicked: only "text",
// which can't see, is enabled (JonahFu on X: OpenRouter turned the unpicked
// Haiku away for their region, and they had never enabled it).
func routerUp(t *testing.T, picks ...string) (*Server, *eyeUp) {
	t.Helper()
	fresh(t)
	u := &eyeUp{}
	up := httptest.NewServer(u)
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "router", Name: "Router", Chat: up.URL + "/v1", Key: "key", Models: picks}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("router", up.URL+"/v1", []catalog.Model{
		{ID: "text", ImageInput: imageInputBool(false), Price: &catalog.Price{Input: 0.1, Output: 0.1}},
		{ID: "anthropic/claude-haiku-5.5", Images: true, ImageInput: imageInputBool(true), Price: &catalog.Price{Input: 1, Output: 5}},
		{ID: "google/gemini-3-flash", Images: true, ImageInput: imageInputBool(true), Price: &catalog.Price{Input: 0.3, Output: 2.5}},
	}); err != nil {
		t.Fatal(err)
	}
	return New(), u
}

func TestAutoVisionPicksOnlyAnEnabledModel(t *testing.T) {
	s, u := routerUp(t, "text")
	if m := AutoVision(); m != "" {
		t.Fatalf("AutoVision = %q, a model the user never enabled", m)
	}
	code, body := postAs(t, s, "", `{"model":"router/text","messages":[{"role":"user","content":[{"type":"text","text":"read"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8="}}]}]}`)
	if code != 400 || !strings.Contains(body, "none of the models enabled in uni-switch can see images") {
		t.Fatalf("%d %s", code, body)
	}
	if got := u.sent("anthropic/claude-haiku-5.5"); len(got) != 0 {
		t.Fatalf("the unpicked model was asked to describe: %v", got)
	}

	// with one that sees enabled, that one describes, not the cheaper
	// small model the user left out
	routerUp(t, "text", "anthropic/claude-haiku-5.5")
	if m := AutoVision(); m != "router/anthropic/claude-haiku-5.5" {
		t.Fatalf("AutoVision = %q", m)
	}
}

// The web-search side request's own pick is the same: a model the user
// enabled.
func TestSearcherModelIsAnEnabledModel(t *testing.T) {
	routerUp(t, "text")
	p, err := provider.Find("router")
	if err != nil {
		t.Fatal(err)
	}
	if m := searcherModel(*p); m != "text" {
		t.Fatalf("searcherModel = %q, want the only enabled model", m)
	}
	if picked := slices.ContainsFunc(p.Exposed(), func(m catalog.Model) bool { return m.ID == "google/gemini-3-flash" }); picked {
		t.Fatal("gemini is enabled")
	}
}
