package gui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Many keys at once (361 on Discord: a provider added with hundreds of
// keys): pasted into the key field of a new provider, the first is its key
// and the others its accounts, each once; and keys/remove-many removes the
// ones named in one go, saying how many.
func TestProviderSaveSplitsKeysAndRemovesMany(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	post := func(path, body string) string {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(body)))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		return w.Body.String()
	}
	post("/api/provider/save", `{"id":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","key":"sk-a, sk-b, sk-c, sk-b","new":true}`)
	p, err := provider.Find("relay")
	if err != nil || p.Key != "sk-a" || len(p.Keys) != 2 || p.Keys[0].Key != "sk-b" || p.Keys[1].Key != "sk-c" || p.Keys[0].Off {
		t.Fatalf("%v: key %q, others %+v", err, p.Key, p.Keys)
	}
	// saved again with some it has and one new: the first replaces the
	// key, the new one is added, those it has stay once
	post("/api/provider/save", `{"id":"relay","from":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","key":"sk-a\nsk-c\nsk-d"}`)
	p, _ = provider.Find("relay")
	if got := len(p.KeyList()); p.Key != "sk-a" || got != 4 {
		t.Fatalf("key %q, %d keys: %+v", p.Key, got, p.Keys)
	}
	// one key alone is saved as it is
	post("/api/provider/save", `{"id":"solo","name":"Solo","chat":"http://127.0.0.1:1/v1","key":"sk-only","new":true}`)
	if p, _ := provider.Find("solo"); p.Key != "sk-only" || len(p.Keys) != 0 {
		t.Fatalf("solo %q %+v", p.Key, p.Keys)
	}
	body := post("/api/keys/remove-many", `{"id":"relay","refs":["`+provider.KeyID("sk-a")+`","`+provider.KeyID("sk-c")+`"]}`)
	if !strings.Contains(body, `"removed":2`) {
		t.Fatalf("answer %s", body)
	}
	if p, _ := provider.Find("relay"); p.Key != "sk-b" || len(p.Keys) != 1 || p.Keys[0].Key != "sk-d" {
		t.Fatalf("left %q %+v", p.Key, p.Keys)
	}
}
