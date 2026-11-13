package gui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// jiabubu on Discord: the editor's "Count its models as free" is saved on
// a preset (ModelScope, whose free daily quota is the case) and on a
// provider the user typed in, given back by /api/providers for the editor
// to open with, and turned off by a save that says so.
func TestProviderSaveKeepsCountFree(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	do := func(method, path, body string) string {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		return w.Body.String()
	}
	do("POST", "/api/provider/save", `{"id":"modelscope","name":"ModelScope","preset":"modelscope","key":"ms-k","countFree":true,"new":true}`)
	if p, err := provider.Find("modelscope"); err != nil || !p.CountFree || !p.FreeModel("any-model-of-it") {
		t.Fatalf("the preset's mark wasn't kept: %+v %v", p, err)
	}
	if !strings.Contains(do("GET", "/api/providers", ""), `"countFree":true`) {
		t.Fatal("/api/providers doesn't give the mark back")
	}
	do("POST", "/api/provider/save", `{"id":"modelscope","from":"modelscope","name":"ModelScope","preset":"modelscope","key":"","countFree":false}`)
	if p, err := provider.Find("modelscope"); err != nil || p.CountFree {
		t.Fatalf("turned off, it stayed: %+v %v", p, err)
	}

	do("POST", "/api/provider/save", `{"id":"box","name":"Box","chat":"http://127.0.0.1:9/v1","key":"k","countFree":true,"new":true}`)
	if p, err := provider.Find("box"); err != nil || !p.CountFree {
		t.Fatalf("a typed-in provider's mark wasn't kept: %+v %v", p, err)
	}
}
