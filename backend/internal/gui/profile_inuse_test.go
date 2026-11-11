package gui

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The page's state says which profile is in use (superhtoooth on X: the
// panel's button read 方案 1, 方案 2, a count taken for the name, and
// nothing said which profile was in use). Through the page's own calls: a
// profile saved under a name is in use at once, another applied is, and a
// model changed by hand since leaves none in use.
func TestStateProfileInUse(t *testing.T) {
	home := sandboxHome(t)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("model = \"relay/m1\"\n"), 0o600)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1", "m2"}}); err != nil {
		t.Fatal(err)
	}
	h := Handler(nil, nil)
	call := func(path, body string) []string {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("%s %d %s", path, rec.Code, rec.Body)
		}
		var s stateJSON
		if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
			t.Fatal(err)
		}
		var on []string
		for _, p := range s.Profiles {
			if p.InUse {
				on = append(on, p.Name)
			}
		}
		return on
	}
	eq := func(got []string, want ...string) bool { return strings.Join(got, ",") == strings.Join(want, ",") }

	if got := call("/api/profile/save", `{"name":"工作"}`); !eq(got, "工作") {
		t.Fatalf("saved 工作: in use %v", got)
	}
	if got := call("/api/set", `{"agent":"codex","field":"model","value":"relay/m2"}`); len(got) != 0 {
		t.Fatalf("model changed by hand: in use %v", got)
	}
	if got := call("/api/profile/save", `{"name":"游戏"}`); !eq(got, "游戏") {
		t.Fatalf("saved 游戏: in use %v", got)
	}
	if got := call("/api/profile/use", `{"name":"工作"}`); !eq(got, "工作") {
		t.Fatalf("applied 工作: in use %v", got)
	}
}
