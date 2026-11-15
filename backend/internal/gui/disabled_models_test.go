package gui

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agentenv"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// Settings' disabled models (#1867) are set on their own: the page is told
// each pattern with the models it turns off, a disabled model leaves the
// models it may pick, and the page's other saves, which never send the
// list, keep it.
func TestDisabledModelsSetting(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	t.Setenv("APPDATA", filepath.Join(h, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(h, "AppData", "Local"))
	for _, v := range agentenv.Vars {
		t.Setenv(v, "")
	}
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Key: "k", Models: []string{"m1", "claude-fable-5"}, Chat: "http://127.0.0.1:9/v1"}); err != nil {
		t.Fatal(err)
	}
	type state struct {
		DisabledModels []string              `json:"disabledModels"`
		DisabledHits   map[string][]string   `json:"disabledHits"`
		TitleModels    []struct{ ID string } `json:"titleModels"`
	}
	call := func(path, body string) (int, state) {
		t.Helper()
		rec := httptest.NewRecorder()
		Handler(nil, nil).ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
		var out state
		json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	ids := func(s state) []string {
		var out []string
		for _, m := range s.TitleModels {
			out = append(out, m.ID)
		}
		return out
	}
	code, out := call("/api/settings/disabled-models", `{"models":["*fable*"," ","*FABLE*"]}`)
	if code != 200 || !slices.Equal(out.DisabledModels, []string{"*fable*"}) || !slices.Equal(out.DisabledHits["*fable*"], []string{"fake/claude-fable-5"}) {
		t.Fatalf("%d %+v", code, out)
	}
	if got := ids(out); slices.Contains(got, "fake/claude-fable-5") || !slices.Contains(got, "fake/m1") {
		t.Fatalf("pickable %v", got)
	}
	if code, _ := call("/api/settings", `{"theme":"dark"}`); code != 200 || !slices.Equal(settings.Load().DisabledModels, []string{"*fable*"}) {
		t.Fatalf("another save (%d) lost the list: %v", code, settings.Load().DisabledModels)
	}
	if code, _ := call("/api/settings", `{"theme":"dark","disabledModels":[]}`); code != 200 || !slices.Equal(settings.Load().DisabledModels, []string{"*fable*"}) {
		t.Fatalf("the page's save cleared the list (%d): %v", code, settings.Load().DisabledModels)
	}
	if code, out := call("/api/settings/disabled-models", `{"models":[]}`); code != 200 || len(out.DisabledModels) != 0 || !slices.Contains(ids(out), "fake/claude-fable-5") {
		t.Fatalf("enabled: %d %+v", code, out)
	}
}
