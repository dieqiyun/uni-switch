package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The route the Codex app's app-server is restarted on (codex_app_server.go)
// changes when Codex is connected beside its ChatGPT sign-in and when it is
// disconnected (JiangLiuer222 on X: the app kept sending to 127.0.0.1:3425
// after Disconnect), and not when the Codex app writes a model pick of its
// own, which would restart it on every pick.
func TestCodexAppRouteFollowsConnectAndDisconnect(t *testing.T) {
	home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, "model = \"gpt-5.5\"\n")
	cx := codex(home)
	route := func() string {
		t.Helper()
		r, _, known := codexAppRoute()
		if !known {
			t.Fatal("route not known")
		}
		return r
	}
	before := route()
	if _, err := cx.ConnectHow(); err != nil {
		t.Fatal(err)
	}
	connected := route()
	if connected == before {
		t.Fatalf("connect left the route as it was:\n%s", read())
	}
	// the Codex app writes its own pick: no restart for that
	if err := edit.SetTOMLTop(filepath.Join(home, ".codex", "config.toml"), edit.KV{Path: "model", Value: "gpt-6.1-sol"}); err != nil {
		t.Fatal(err)
	}
	if route() != connected {
		t.Fatalf("a model pick changed the route:\n%s", read())
	}
	if err := cx.Disconnect(); err != nil {
		t.Fatal(err)
	}
	// the [model_providers.uni-switch] table stays, for the threads started
	// on it; openai_base_url goes
	if r := route(); r == connected {
		t.Fatalf("disconnect left the route as it was: %q\n%s", r, read())
	}
}

// requires_openai_auth on uni-switch's table decides whether the Codex app
// has its ChatGPT account at all, and the app-server reads it
// as it starts: the route changes with it, so the app is restarted onto
// its account when Sync writes it (Qyburn3 on X).
func TestCodexAppRouteFollowsOpenAIAuth(t *testing.T) {
	home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, "")
	cx := codex(home)
	if err := cx.Field("login").Set("api"); err != nil {
		t.Fatal(err)
	}
	if err := cx.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	with, _, _ := codexAppRoute()
	if err := edit.SetTOMLKey(cx.Path, "model_providers."+uniSwitchID, "requires_openai_auth", false); err != nil {
		t.Fatal(err)
	}
	without, _, _ := codexAppRoute()
	if without == with {
		t.Fatalf("requires_openai_auth taken out left the route as it was:\n%s", read())
	}
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if restored, _, _ := codexAppRoute(); restored != with {
		t.Fatalf("sync did not restore the authenticated route:\n%s", read())
	}
	codexUsedUp = func() bool { return true }
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if spent, _, _ := codexAppRoute(); spent != with {
		t.Fatalf("spent account changed the authenticated route:\n%s", read())
	}
}

// #4550: the Codex app's app-server was restarted three times in 20
// minutes with the user doing nothing, and a slow MCP server took
// ChatGPT down with each. What uni-switch writes to Codex's catalog changes
// with no change of the user's: a list that fails to fetch drops its
// models and has them back at the next fetch, the order is re-ranked by
// the models Codex used last, descriptions and windows follow the
// vendor's list. None of that is where Codex sends, so none of it is the
// route, which restarts the app-server at once; and a catalog that can't
// be read now is no change either.
func TestCodexAppRouteKeepsThroughListFlaps(t *testing.T) {
	home := syncHome(t)
	codexCat := filepath.Join(home, ".codex", "uni-switch-models.json")
	writeFile(t, filepath.Join(home, ".codex", "config.toml"), "model = \"relay/glm-4.6\"\nmodel_provider = \"uni-switch\"\nmodel_catalog_json = '"+codexCat+"'\n")
	writeFile(t, codexCat, `{"models":[]}`)
	save := func(models ...string) {
		t.Helper()
		if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: models}); err != nil {
			t.Fatal(err)
		}
		SyncCatalog()
	}
	route := func() string {
		t.Helper()
		r, _, known := codexAppRoute()
		if !known {
			t.Fatal("route not known")
		}
		return r
	}
	save("glm-4.6", "glm-4.5")
	full := readFile(codexCat)
	if !strings.Contains(full, "glm-4.5") {
		t.Fatalf("catalog:\n%s", full)
	}
	before := route()
	// the list fetched while the vendor failed: one model left
	save("glm-4.6")
	if strings.Contains(readFile(codexCat), "glm-4.5") {
		t.Fatalf("catalog still lists the dropped model:\n%s", readFile(codexCat))
	}
	if r := route(); r != before {
		t.Errorf("a model dropped from the list changed the route:\n%s\nto\n%s", before, r)
	}
	// and back at the next fetch
	save("glm-4.6", "glm-4.5")
	if r := route(); r != before {
		t.Errorf("the model back in the list changed the route:\n%s\nto\n%s", before, r)
	}
	// the same models in another order, as usage re-ranks them, with
	// other descriptions
	var cat map[string][]map[string]any
	if err := json.Unmarshal([]byte(readFile(codexCat)), &cat); err != nil {
		t.Fatal(err)
	}
	ms := cat["models"]
	slices.Reverse(ms)
	for i, m := range ms {
		m["priority"] = i + 1
		m["description"] = "fetched again"
	}
	b, _ := json.Marshal(cat)
	writeFile(t, codexCat, string(b))
	if r := route(); r != before {
		t.Errorf("the list in another order changed the route:\n%s\nto\n%s", before, r)
	}
	// half a file, as another writer's would be mid-write: not known,
	// never a change
	writeFile(t, codexCat, `{"models":[{"slug":"glm`)
	if r := route(); r != before {
		t.Errorf("a catalog that can't be read changed the route:\n%s\nto\n%s", before, r)
	}
}

// Asked every 3s, the route reads Codex's config.toml through filememo
// alone: Codex keeps every project's trust level there, and a live 1.5 MB
// one read whole on each look was 1.55 GB in 53 minutes. A config that
// can't be read is still an unknown route.
func TestCodexAppRouteReadsNoWholeConfig(t *testing.T) {
	var cfg strings.Builder
	cfg.WriteString("model = \"gpt-5.5\"\n")
	for i := range 20000 {
		fmt.Fprintf(&cfg, "[projects.\"/Users/someone/work/project-%05d\"]\ntrust_level = \"trusted\"\n", i)
	}
	home, _ := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, cfg.String())
	path := filepath.Join(home, ".codex", "config.toml")
	if fi, err := os.Stat(path); err != nil || fi.Size() < 1<<20 {
		t.Fatalf("config of %v bytes: %v", fi.Size(), err)
	}
	// written a while ago, as the live one: filememo compares the bytes of
	// a file written just now
	old := time.Now().Add(-time.Hour)
	os.Chtimes(path, old, old)
	if _, _, known := codexAppRoute(); !known {
		t.Fatal("route not known")
	}
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	for range 10 {
		codexAppRoute()
	}
	runtime.ReadMemStats(&m1)
	if n := m1.TotalAlloc - m0.TotalAlloc; n > 1<<20 {
		t.Fatalf("10 looks at an unchanged config allocated %d bytes", n)
	}
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		return // a mode doesn't stop the read
	}
	os.Chmod(path, 0)
	defer os.Chmod(path, 0o600)
	if _, _, known := codexAppRoute(); known {
		t.Fatal("a config that can't be read gave a route")
	}
}
