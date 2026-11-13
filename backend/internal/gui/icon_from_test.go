package gui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// hoobnn on Discord: a custom provider's icon fetched from an image URL
// had its address gone when the editor was opened again. The address is
// saved with the picture, given back by /api/providers, kept by a save
// that doesn't name the icon, and dropped with a picture that came from
// elsewhere.
func TestProviderSaveKeepsIconFrom(t *testing.T) {
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
	const from = "https://cdn.example.com/logo.png"
	do("POST", "/api/provider/save", `{"id":"box","name":"Box","chat":"http://127.0.0.1:9/v1","key":"k","icon":"file:00000000000000bb.png","iconFrom":"`+from+`","new":true}`)
	if p, err := provider.Find("box"); err != nil || p.Icon != "file:00000000000000bb.png" || p.IconFrom != from {
		t.Fatalf("the icon's address wasn't kept: %+v %v", p, err)
	}
	if !strings.Contains(do("GET", "/api/providers", ""), `"iconFrom":"`+from+`"`) {
		t.Fatal("/api/providers doesn't give the address back")
	}
	// a save that doesn't name the icon (the CLI's, the TUI's) keeps both
	do("POST", "/api/provider/save", `{"id":"box","from":"box","name":"Box","chat":"http://127.0.0.1:9/v1","key":""}`)
	if p, err := provider.Find("box"); err != nil || p.Icon != "file:00000000000000bb.png" || p.IconFrom != from {
		t.Fatalf("a save without the icon lost it: %+v %v", p, err)
	}
	// a built-in icon picked after: the address isn't that picture's
	do("POST", "/api/provider/save", `{"id":"box","from":"box","name":"Box","chat":"http://127.0.0.1:9/v1","key":"","icon":"openai","iconFrom":"`+from+`"}`)
	if p, err := provider.Find("box"); err != nil || p.Icon != "openai" || p.IconFrom != "" {
		t.Fatalf("the address outlived its picture: %+v %v", p, err)
	}
}
