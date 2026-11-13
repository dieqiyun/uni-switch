package gui

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// Settings › Network › Model ids without the provider posts
// settings/bare-ids on its own (love1sbug on X): it is saved, the page is
// answered the settings with it, and off saves it off again.
func TestBareModelIDsSetting(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	for _, on := range []bool{true, false} {
		rec := httptest.NewRecorder()
		body := `{"on":false}`
		if on {
			body = `{"on":true}`
		}
		Handler(nil, nil).ServeHTTP(rec, httptest.NewRequest("POST", "/api/settings/bare-ids", strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		var got struct {
			BareModelIDs bool `json:"bareModelIDs"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.BareModelIDs != on || settings.Load().BareModelIDs != on {
			t.Fatalf("on %v: answered %v, saved %v", on, got.BareModelIDs, settings.Load().BareModelIDs)
		}
	}
}
