package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Each of a provider's several keys copies its own (#1480, IamMiao): the
// editor asks provider/key with the row's key id and gets that key, the
// first's included; with no id, the first as before; an id it hasn't is
// an error, never another key.
func TestEachKeyCopiesItsOwn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	post := func(path, body string) (int, string) {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(body)))
		return w.Code, w.Body.String()
	}
	if code, body := post("/api/provider/save", `{"id":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","key":"sk-a, sk-b, sk-c","new":true}`); code != 200 {
		t.Fatalf("save: %d %s", code, body)
	}
	keyOf := func(ref string) (int, string) {
		t.Helper()
		in, _ := json.Marshal(map[string]string{"id": "relay", "account": ref})
		code, body := post("/api/provider/key", string(in))
		var out struct{ Key string }
		json.Unmarshal([]byte(body), &out)
		return code, out.Key
	}
	for _, k := range []string{"sk-a", "sk-b", "sk-c"} {
		if code, got := keyOf(provider.KeyID(k)); code != 200 || got != k {
			t.Errorf("row %s copies %q (%d)", k, got, code)
		}
	}
	if code, got := keyOf(""); code != 200 || got != "sk-a" {
		t.Errorf("no row: %q (%d), want the first", got, code)
	}
	if code, got := keyOf(provider.KeyID("sk-gone")); code == 200 || got != "" {
		t.Errorf("a key it hasn't: %q (%d)", got, code)
	}
}
