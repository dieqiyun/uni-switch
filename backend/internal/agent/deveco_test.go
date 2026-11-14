package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// DevEco Code (#1885) keeps OpenCode's config shape in ~/.config/deveco:
// the first of deveco.jsonc, deveco.json and config.json there is the one
// it reads last, so the one uni-switch writes; DEVECO_CONFIG_DIR, read after
// all of them, when set to an absolute folder.
func TestDevEcoConfigAsItFindsIt(t *testing.T) {
	home := syncHome(t)
	cfg := filepath.Join(home, ".config")
	dir := filepath.Join(cfg, "deveco")
	if a := deveco(home, cfg); a.Path != filepath.Join(dir, "deveco.json") {
		t.Fatalf("with no config, %s", a.Path)
	}
	writeFile(t, filepath.Join(dir, "config.json"), `{}`)
	if a := deveco(home, cfg); a.Path != filepath.Join(dir, "config.json") {
		t.Fatalf("with config.json only, %s", a.Path)
	}
	// the one DevEco Code makes on its first start, which wins its merge
	writeFile(t, filepath.Join(dir, "deveco.jsonc"), `{"$schema":"https://opencode.ai/config.json"}`)
	if a := deveco(home, cfg); a.Path != filepath.Join(dir, "deveco.jsonc") {
		t.Fatalf("with deveco.jsonc, %s", a.Path)
	}

	moved := filepath.Join(home, "deveco-cfg")
	t.Setenv("DEVECO_CONFIG_DIR", moved)
	if a := deveco(home, cfg); a.Path != filepath.Join(moved, "deveco.json") {
		t.Fatalf("with DEVECO_CONFIG_DIR, config at %s", a.Path)
	}
	t.Setenv("DEVECO_CONFIG_DIR", "deveco-cfg")
	if a := deveco(home, cfg); a.Path != filepath.Join(dir, "deveco.jsonc") {
		t.Fatalf("with a relative DEVECO_CONFIG_DIR, config at %s", a.Path)
	}
}

// Parts of DevEco Code's code still send OpenCode's own User-Agent
// (opencode/<version>), so the key uni-switch writes for it names it: its usage
// is DevEco Code's, not OpenCode's, whichever it sends. OpenCode's own
// stays the gateway's plain key.
func TestDevEcoKeyNamesIt(t *testing.T) {
	syncHome(t)
	key := func(id string) any {
		return uniSwitchProviderJSONFor("opencode", id).(map[string]any)["options"].(map[string]any)["apiKey"]
	}
	if got := key("deveco"); got != gateway.TokenFor("deveco") {
		t.Errorf("deveco's key %v", got)
	}
	if got := key("opencode"); got != gateway.Token {
		t.Errorf("opencode's key %v", got)
	}
}

// Connected, uni-switch's provider goes in beside the user's own, with
// DevEco Code's own key; its built-in provider, the user's own provider,
// their small model and the file's $schema stay as they were. Disconnected,
// the model they were on comes back.
func TestDevEcoConnectKeepsTheUsersOwn(t *testing.T) {
	home := syncHome(t)
	cfg := filepath.Join(home, ".config", "deveco", "deveco.jsonc")
	writeFile(t, cfg, `{
  "$schema": "https://opencode.ai/config.json",
  // mine
  "model": "deveco/glm-5",
  "small_model": "mine/x",
  "provider": {"mine": {"name": "mine", "options": {"baseURL": "http://127.0.0.1:9/v1", "apiKey": "sk-own"}}}
}`)
	a, err := Find("deveco")
	if err != nil {
		t.Fatal(err)
	}
	if a.Path != cfg {
		t.Fatalf("config at %s", a.Path)
	}
	if a.Wired() {
		t.Fatal("wired before connecting")
	}
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	get := func(k string) string { v, _ := edit.GetJSON(cfg, k); return v }
	if m := get("model"); !strings.HasPrefix(m, uniSwitchID+"/") {
		t.Fatalf("model %q after connecting", m)
	}
	if k := get("provider.uni-switch.options.apiKey"); k != gateway.TokenFor("deveco") {
		t.Fatalf("apiKey %q", k)
	}
	if u := get("provider.uni-switch.options.baseURL"); u != gateway.URL()+"/v1" {
		t.Fatalf("baseURL %q", u)
	}
	if k := get("provider.mine.options.apiKey"); k != "sk-own" {
		t.Fatalf("own provider %q", k)
	}
	if s := get("small_model"); s != "mine/x" {
		t.Fatalf("small_model %q", s)
	}
	if s := get("$schema"); s != "https://opencode.ai/config.json" {
		t.Fatalf("$schema %q", s)
	}
	if !a.Wired() {
		t.Fatal("not wired after connecting")
	}
	if c := a.Check(); c != "" {
		t.Fatalf("check after connecting: %s", c)
	}
	// a key that isn't DevEco Code's is told as off
	if err := edit.SetJSON(cfg, edit.KV{Path: "provider.uni-switch.options.apiKey", Value: gateway.Token}); err != nil {
		t.Fatal(err)
	}
	if a.Check() == "" {
		t.Fatal("check passes the plain key")
	}
	// and a sync puts DevEco Code's back
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if k := get("provider.uni-switch.options.apiKey"); k != gateway.TokenFor("deveco") {
		t.Fatalf("apiKey %q after a sync", k)
	}

	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if m := get("model"); m != "deveco/glm-5" {
		t.Fatalf("model %q after disconnecting", m)
	}
	if s := get("small_model"); s != "mine/x" {
		t.Fatalf("small_model %q after disconnecting", s)
	}
	if k := get("provider.mine.options.apiKey"); k != "sk-own" {
		t.Fatalf("own provider %q after disconnecting", k)
	}
	if _, ok := edit.GetJSON(cfg, "provider.uni-switch"); ok {
		t.Fatalf("uni-switch's provider left: %s", readFile(cfg))
	}
}
