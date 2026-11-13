package gui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #2008: the editor knows nothing of the time of day a provider's daily
// allowance comes back, set by `uni-switch provider quota-rest <id> until
// 08:00`, nor of its rest cap (`uni-switch provider rest-cap`), so its save
// keeps both.
func TestSaveKeepsQuotaResetAt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := provider.Save(provider.Provider{ID: "reset-test", Name: "Reset", Chat: "https://example.invalid/v1", Key: "k"}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetQuotaResetAt("reset-test", "00:00+08:00"); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetRestCap("reset-test", 240); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/save", strings.NewReader(`{"id":"reset-test","name":"Reset","chat":"https://example.invalid/v1"}`)))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	got, err := provider.Find("reset-test")
	if err != nil {
		t.Fatal(err)
	}
	if got.QuotaResetAt != "00:00+08:00" || got.RestCap != 240 || got.Name != "Reset" {
		t.Fatalf("the save dropped the reset time %q or the rest cap %d", got.QuotaResetAt, got.RestCap)
	}
}
