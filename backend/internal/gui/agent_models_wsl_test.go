package gui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The models picked on a WSL agent's row (Claude Code · WSL Ubuntu) are the
// ones its pickers offer and the line counts: they are kept under the id
// it is written uni-switch's models with and sends the gateway's key of, its
// Windows twin's (#927). Kept under claude@wsl:Ubuntu, the line said 2
// while the default-model picker, the agent's own menu and the gateway's
// list still had every model.
func TestAgentModelsWSL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "home", "me", ".claude"), 0o755)
	os.WriteFile(filepath.Join(root, "home", "me", ".claude", "settings.json"), []byte("{}\n"), 0o644)
	t.Cleanup(agent.FakeWSL(map[string]string{"Ubuntu": "home:/home/me\ndir:.claude\nbin:claude\n"},
		map[string]string{"Ubuntu": root}))
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1", "m2", "m3"}}); err != nil {
		t.Fatal(err)
	}
	const id = "claude@wsl:Ubuntu"
	if _, err := agent.Find(id); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	agentModelsAPI(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/agent-models/"+id, strings.NewReader(`{"hidden":["relay/m2","relay/m3"]}`)))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	// what Claude Code in the distro is written and the gateway lists to
	// its key
	shown, _ := provider.CatalogFor("claude")
	if len(shown) != 1 || shown[0].ID != "relay/m1" {
		t.Fatalf("Claude Code is shown %v", shown)
	}
	a, _ := agent.Find(id)
	refs := map[string]bool{}
	for _, f := range agentFields(a, a.Values()) {
		for _, o := range f.Options {
			if o.Ref != "" {
				refs[o.Ref] = true
			}
		}
	}
	if !refs["relay/m1"] || refs["relay/m2"] || refs["relay/m3"] {
		t.Fatalf("the row's pickers offer %v", refs)
	}
	if c := agentModelCount(a, agentFields(a, a.Values())); c == nil || c.Shown != 1 || c.Listed != 3 {
		t.Fatalf("the line counts %+v", c)
	}
	if ms := agentModelList(a); len(ms) != 3 || ms[0].Hidden || !ms[1].Hidden || !ms[2].Hidden {
		t.Fatalf("the list %+v", ms)
	}
}
