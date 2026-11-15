package gui

import (
	"net/http"
	"net/http/httptest"

	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/access"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

func TestSettingsMigratesOldLANCredentialToManagedKey(t *testing.T) {
	sandboxHome(t)
	old := "sk-uni-switch-0123456789abcdef0123456789abcdef01234567"
	s := settings.Load()
	s.LANKey = old
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	handler := Handler(nil, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), old) || strings.Contains(w.Body.String(), `"lanKey"`) {
		t.Fatal("old credential leaked", w.Code, w.Body)
	}
	if settings.Load().LANKey != old {
		t.Fatal("migration did not retain the old credential for older uni-switch")
	}
	keys, _ := access.List()
	if len(keys) != 1 || keys[0].Name != "uni-switch" || settings.Load().LANKeyID != keys[0].ID {
		t.Fatal(keys)
	}
	who, ok := access.Authenticate(old)
	if !ok || who.KeyID != keys[0].ID {
		t.Fatal(who, ok)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/caller-keys", nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), old) {
		t.Fatal("list exposed credential", w.Code, w.Body)
	}
}
