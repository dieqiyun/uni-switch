package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// io.uni-switch.desktop on an Omarchy machine asks the uni-switch there for the theme it
// draws itself in: only from this machine, only readable by the site.
func TestOmarchyThemeForTheSite(t *testing.T) {
	fresh(t)
	t.Setenv("UNI_SWITCH_ADDR", "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "colors.toml"), []byte("mode = \"light\"\naccent = \"#1e66f5\"\nbackground = \"#eff1f5\"\nforeground = \"#4c4f69\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UNI_SWITCH_OMARCHY_THEME", dir)
	t.Setenv("UNI_SWITCH_OMARCHY_FONT", "iA Writer Mono S")
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	was := onOmarchy
	t.Cleanup(func() { onOmarchy = was })
	h := localGuard(New().Handler())
	call := func(method, from, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/v1/uni-switch/omarchy", nil)
		r.RemoteAddr = from
		r.Host = "127.0.0.1:3425"
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if method == "OPTIONS" {
			r.Header.Set("Access-Control-Request-Method", "GET")
			r.Header.Set("Access-Control-Request-Private-Network", "true")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	onOmarchy = false
	if w := call("GET", "127.0.0.1:5000", "https://github.com/dieqiyun/uni-switch"); w.Code != http.StatusNotFound {
		t.Fatal("not on Omarchy:", w.Code, w.Body)
	}

	onOmarchy = true
	w := call("GET", "127.0.0.1:5000", "https://github.com/dieqiyun/uni-switch")
	if w.Code != 200 || w.Header().Get("Access-Control-Allow-Origin") != "https://github.com/dieqiyun/uni-switch" {
		t.Fatal("the site, on this machine:", w.Code, w.Header(), w.Body)
	}
	var th struct {
		Mode string
		Vars map[string]string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &th); err != nil {
		t.Fatal(err)
	}
	if th.Mode != "light" || th.Vars["--bg"] != "#eff1f5" || th.Vars["--accent"] != "#1e66f5" || th.Vars["--om-radius"] != "0px" {
		t.Fatal("theme:", th.Mode, th.Vars)
	}
	if f := th.Vars["--font"]; len(f) < 18 || f[:18] != `"iA Writer Mono S"` {
		t.Fatal("font:", f)
	}

	if w := call("GET", "127.0.0.1:5000", "https://evil.example"); w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("another site may read it:", w.Header())
	}
	if w := call("GET", "192.168.1.9:5000", ""); w.Code == 200 {
		t.Fatal("another machine got it:", w.Code)
	}
	w = call("OPTIONS", "127.0.0.1:5000", "https://github.com/dieqiyun/uni-switch")
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Private-Network") != "true" {
		t.Fatal("preflight:", w.Code, w.Header())
	}
}
