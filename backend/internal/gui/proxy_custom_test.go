package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// Custom proxy remembered (Zane on Discord): a proxy of the user's own,
// login and all, is kept as proxyCustom when Off ("direct") or Auto ("")
// is chosen after it, so Settings' Custom can bring it back, and the page
// is told it. Only the proxy sets it: a proxyCustom the page sends is not
// taken, and neither Off nor Auto changes what the gateway's requests go
// through.
func TestProxyCustomRemembered(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	srv := Handler(nil, nil)
	post := func(body string) map[string]any {
		t.Helper()
		rec := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/settings", strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:50000"
		srv.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", body, rec.Code, rec.Body)
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	own := "socks5://n%40me:p%40ss@127.0.0.1:1080"
	if out := post(`{"theme":"dark","lang":"en","proxy":"` + own + `"}`); out["proxyCustom"] != own {
		t.Fatalf("custom saved, page told %v", out["proxyCustom"])
	}
	for _, other := range []string{"direct", ""} {
		out := post(`{"theme":"dark","lang":"en","proxy":"` + other + `","proxyCustom":"http://page.example:1"}`)
		if s := settings.Load(); s.Proxy != other || s.ProxyCustom != own || out["proxyCustom"] != own {
			t.Fatalf("proxy %q: saved %q, remembered %q, page told %v", other, s.Proxy, s.ProxyCustom, out["proxyCustom"])
		}
	}
	next := "http://127.0.0.1:7890"
	post(`{"theme":"dark","lang":"en","proxy":"` + next + `"}`)
	if s := settings.Load(); s.Proxy != next || s.ProxyCustom != next {
		t.Fatalf("a new custom proxy: saved %q, remembered %q", s.Proxy, s.ProxyCustom)
	}
}
