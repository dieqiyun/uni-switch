package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// With an auto-review model picked (#938), every entry of the list a
// signed-in Codex is handed — the ChatGPT backend's and uni-switch's — names it
// as auto_review_model_override, under another ETag; unset, the backend's
// entries are as it gave them, an override of its own kept, and uni-switch's
// have none.
func TestCodexModelsAutoReview(t *testing.T) {
	setup(t, provider.Chat, &fake{t: t})
	chatgpt(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `W/"up"`)
		w.Write([]byte(`{"models":[{"slug":"gpt-6","priority":1,"auto_review_model_override":"codex-auto-review"},{"slug":"gpt-5.5","priority":2}]}`))
	})
	t.Setenv("XDG_CONFIG_HOME", "")
	list := func() (string, map[string]map[string]any) {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", CodexPath+"/models?client_version=0.159.2", nil)
		req.Header.Set("Authorization", "Bearer chatgpt-token")
		New().Handler().ServeHTTP(rec, req)
		var l struct {
			Models []map[string]any `json:"models"`
		}
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &l) != nil {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		by := map[string]map[string]any{}
		for _, m := range l.Models {
			by[m["slug"].(string)] = m
		}
		return rec.Header().Get("ETag"), by
	}
	has := func(m map[string]any) any {
		v, ok := m["auto_review_model_override"]
		if !ok {
			return "none"
		}
		return v
	}

	tagOff, off := list()
	if has(off["gpt-6"]) != "codex-auto-review" || has(off["gpt-5.5"]) != "none" || has(off["fake/m1"]) != "none" {
		t.Fatalf("unset: %v", off)
	}
	if err := provider.SetCodexAutoReview("fake/m1"); err != nil {
		t.Fatal(err)
	}
	tagOn, on := list()
	if len(on) != len(off) {
		t.Fatalf("the list changed: %v", on)
	}
	for slug, m := range on {
		if has(m) != "fake/m1" {
			t.Fatalf("set: %s says %v", slug, has(m))
		}
	}
	if tagOn == tagOff {
		t.Fatalf("ETag %q unchanged", tagOn)
	}
	if err := provider.SetCodexAutoReview(""); err != nil {
		t.Fatal(err)
	}
	tagBack, back := list()
	if tagBack != tagOff || has(back["gpt-6"]) != "codex-auto-review" || has(back["gpt-5.5"]) != "none" || has(back["fake/m1"]) != "none" {
		t.Fatalf("unset again %q: %v", tagBack, back)
	}
}
