package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/codexcat"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// With the Settings switch on, the ChatGPT backend's models reach Codex
// saying multi-agent V1 (#141), under another ETag so Codex asks for the
// list again; turned off, the backend's own versions are back, and so is
// the ETag. Nothing else in an entry changes.
func TestCodexModelsAgentsV1(t *testing.T) {
	setup(t, provider.Chat, &fake{t: t})
	chatgpt(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `W/"up"`)
		w.Write([]byte(`{"models":[{"slug":"gpt-6","multi_agent_version":"v2","priority":1},{"slug":"gpt-5.5","priority":2}]}`))
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

	tagOff, off := list()
	if off["gpt-6"]["multi_agent_version"] != "v2" || off["gpt-5.5"]["multi_agent_version"] != nil {
		t.Fatalf("off: %v", off)
	}
	if err := provider.SetCodexAgentsV1(true); err != nil {
		t.Fatal(err)
	}
	tagOn, on := list()
	if on["gpt-6"]["multi_agent_version"] != "v1" || on["gpt-5.5"]["multi_agent_version"] != "v1" || on["gpt-6"]["priority"] != float64(1) {
		t.Fatalf("on: %v", on)
	}
	if on["fake/m1"]["multi_agent_version"] != nil {
		t.Fatalf("a third-party model stamped: %v", on["fake/m1"])
	}
	if tagOn == tagOff || !codexcat.MarkedV1(tagOn) || codexcat.MarkedV1(tagOff) {
		t.Fatalf("ETag %q → %q", tagOff, tagOn)
	}
	h := http.Header{"X-Models-Etag": {`W/"up"`}}
	modelsEtag(h)
	if h.Get("X-Models-Etag") != tagOn {
		t.Fatalf("X-Models-Etag %q, list %q", h.Get("X-Models-Etag"), tagOn)
	}
	if err := provider.SetCodexAgentsV1(false); err != nil {
		t.Fatal(err)
	}
	tagBack, back := list()
	if tagBack != tagOff || back["gpt-6"]["multi_agent_version"] != "v2" || back["gpt-5.5"]["multi_agent_version"] != nil {
		t.Fatalf("off again %q: %v", tagBack, back)
	}
}
