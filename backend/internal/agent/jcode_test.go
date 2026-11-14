package agent

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// jcodeConfig is a config.toml as jcode 0.94.0 writes it after `jcode
// provider add deepseek --base-url … --model deepseek-chat --api-key-stdin
// --set-default` (toml-rs, to_string_pretty), with a comment the user added
// by hand.
const jcodeConfig = `[provider]
# my own pick
default_provider = "deepseek"
default_model = "deepseek-chat"

[providers.deepseek]
type = "openai-compatible"
base_url = "https://api.deepseek.com/v1"
auth = "bearer"
api_key_env = "JCODE_PROVIDER_DEEPSEEK_API_KEY"
env_file = "provider-deepseek.env"
default_model = "deepseek-chat"
requires_api_key = true

[[providers.deepseek.models]]
id = "deepseek-chat"
`

// jcodeHome is a HOME with jcode's ~/.jcode holding body as its
// config.toml, and uni-switch with a provider of two models, one of them with
// effort levels.
func jcodeHome(t *testing.T, body string) (home, path string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("JCODE_HOME", "")
	if err := provider.Save(provider.Provider{ID: "kimi", Name: "Kimi", Key: "k",
		Chat: "https://api.moonshot.cn/v1", Models: []string{"k2", "k2-vision"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("kimi", "https://api.moonshot.cn/v1", []catalog.Model{
		{ID: "k2", Context: 262144, Efforts: []string{"low", "medium", "high"}},
		{ID: "k2-vision", Images: true},
	}); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(home, ".jcode", "config.toml")
	if body != "" {
		writeFile(t, path, body)
	}
	return home, path
}

func jcodeParse(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := toml.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s is not TOML: %v\n%s", path, err, b)
	}
	return m
}

func jcodeTable(m map[string]any, keys ...string) map[string]any {
	for _, k := range keys {
		m, _ = m[k].(map[string]any)
	}
	return m
}

// jcodeModelsOf is uni-switch's profile's models, by id.
func jcodeModelsOf(t *testing.T, m map[string]any) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	list, _ := jcodeTable(m, "providers", "uni-switch")["models"].([]any)
	for _, x := range list {
		e := x.(map[string]any)
		out[e["id"].(string)] = e
	}
	return out
}

// Connecting jcode adds uni-switch's profile — the gateway, with a key naming
// jcode, and every one of uni-switch's models — and starts jcode on the model
// picked. The user's own profile and pick stay in the file, and Disconnect
// gives the file back byte for byte.
func TestJcode(t *testing.T) {
	home, path := jcodeHome(t, jcodeConfig)
	a := jcode(home)
	if a.Path != path || !a.Detected() {
		t.Fatalf("path %q, detected %v", a.Path, a.Detected())
	}
	f := a.Field("model")
	if got := f.Get(); got != "deepseek-chat" || a.Wired() || a.Check() != "" {
		t.Fatalf("before: %q wired %v check %q", got, a.Wired(), a.Check())
	}
	var own, ours bool
	for _, o := range f.Options(a.Values()) {
		own = own || o.Value == "deepseek-chat"
		ours = ours || o.Value == "uni-switch/kimi/k2"
	}
	if !own || !ours {
		t.Fatalf("options: own %v uni-switch %v", own, ours)
	}

	if err := a.Pick("model", "uni-switch/kimi/k2"); err != nil {
		t.Fatal(err)
	}
	m := jcodeParse(t, path)
	if p := jcodeTable(m, "provider"); p["default_provider"] != "uni-switch" || p["default_model"] != "kimi/k2" {
		t.Fatalf("[provider]: %v", p)
	}
	if !a.Wired() || f.Get() != "uni-switch/kimi/k2" {
		t.Fatalf("wired %v, model %q", a.Wired(), f.Get())
	}
	if c := a.Check(); c != "" {
		t.Fatalf("check: %s", c)
	}
	p := jcodeTable(m, "providers", "uni-switch")
	if p["type"] != "openai-compatible" || p["base_url"] != here(home).v1() || p["requires_api_key"] != true ||
		p["api_key"] != agentKeyAt("jcode", here(home).gw()) || p["default_model"] != "kimi/k2" {
		t.Fatalf("uni-switch's profile: %v", p)
	}
	models := jcodeModelsOf(t, m)
	k2, vision := models["kimi/k2"], models["kimi/k2-vision"]
	if len(models) != 2 || k2 == nil || vision == nil {
		t.Fatalf("models: %v", models)
	}
	if k2["context_window"] != int64(262144) || k2["reasoning"] != true || k2["reasoning_effort"] != nil {
		t.Fatalf("k2: %v", k2)
	}
	if vision["context_window"] != int64(jcodeContext) || !reflect.DeepEqual(vision["input"], []any{"text", "image"}) ||
		vision["reasoning"] != nil {
		t.Fatalf("k2-vision: %v", vision)
	}
	b, _ := os.ReadFile(path)
	own0 := jcodeConfig[strings.Index(jcodeConfig, "[providers.deepseek]"):]
	if !strings.Contains(string(b), own0) || !strings.Contains(string(b), "# my own pick\n") {
		t.Fatalf("the user's own profile or comment changed:\n%s", b)
	}

	// switching between uni-switch's models keeps what jcode was on before
	if err := a.Pick("model", "uni-switch/kimi/k2-vision"); err != nil {
		t.Fatal(err)
	}
	if got := f.Get(); got != "uni-switch/kimi/k2-vision" {
		t.Fatalf("model %q", got)
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != jcodeConfig {
		t.Fatalf("Disconnect left:\n%s", got)
	}
	if a.Wired() {
		t.Fatal("still wired")
	}
}

// A model of jcode's own picked while it is on uni-switch takes uni-switch out: the
// profile goes, and jcode is on the user's own provider with that model.
func TestJcodeOwnModelTakesUniSwitchOut(t *testing.T) {
	home, path := jcodeHome(t, jcodeConfig)
	a := jcode(home)
	if err := a.Pick("model", "uni-switch/kimi/k2"); err != nil {
		t.Fatal(err)
	}
	if err := a.Pick("model", "deepseek-reasoner"); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(jcodeConfig, `default_model = "deepseek-chat"`+"\n\n", `default_model = "deepseek-reasoner"`+"\n\n", 1)
	if got, _ := os.ReadFile(path); string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if a.Wired() {
		t.Fatal("still wired")
	}
}

// What the user sets in uni-switch's profile — a header, an effort on one of
// its models — is theirs, also after jcode saved the file itself (its
// models then are [[providers.uni-switch.models]] tables): a catalog sync
// rewrites the models and the endpoint, not those. A sync with nothing to
// change doesn't write.
func TestJcodeSyncKeepsTheirs(t *testing.T) {
	home, path := jcodeHome(t, "")
	a := jcode(home)
	if err := a.Pick("model", "uni-switch/kimi/k2"); err != nil {
		t.Fatal(err)
	}
	gw := here(home)
	// as jcode's Config::save writes it, with what the user changed in it
	saved := `[provider]
default_provider = "uni-switch"
default_model = "kimi/k2"

[providers.uni-switch]
type = "openai-compatible"
base_url = "` + gw.v1() + `"
api_key = "` + agentKeyAt("jcode", gw.gw()) + `"
default_model = "kimi/k2"
requires_api_key = true

[[providers.uni-switch.models]]
id = "kimi/k2"
reasoning = true
reasoning_effort = "high"
context_window = 262144
input = ["text"]

[[providers.uni-switch.models]]
id = "kimi/k2-vision"
context_window = 128000
input = ["text", "image"]

[providers.uni-switch.headers]
X-Team = "mine"
`
	writeFile(t, path, saved)
	// a model uni-switch lists no more leaves jcode's list
	if err := catalog.SaveLive("kimi", "https://api.moonshot.cn/v1", []catalog.Model{
		{ID: "k2", Context: 262144, Efforts: []string{"low", "medium", "high"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "kimi", Name: "Kimi", Key: "k",
		Chat: "https://api.moonshot.cn/v1", Models: []string{"k2"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	m := jcodeParse(t, path)
	models := jcodeModelsOf(t, m)
	if len(models) != 1 || models["kimi/k2"]["reasoning_effort"] != "high" {
		t.Fatalf("models: %v", models)
	}
	if h := jcodeTable(m, "providers", "uni-switch", "headers"); h["X-Team"] != "mine" {
		t.Fatalf("the user's header: %v", h)
	}
	if c := a.Check(); c != "" {
		t.Fatalf("check: %s", c)
	}

	// nothing to change: the file isn't written
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(path); !st.ModTime().Equal(old) {
		t.Fatalf("a sync with nothing to change wrote the file (%v)", st.ModTime())
	}

	// Disconnect takes uni-switch's profile out whole, its array tables and
	// the user's header in it too, and leaves jcode with no pick, as before
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); strings.TrimSpace(string(got)) != "" {
		t.Fatalf("Disconnect left:\n%s", got)
	}
}

// jcode's folder is $JCODE_HOME when it is set; uni-switch's profile gone from
// the file while jcode still starts on it is said.
func TestJcodeHome(t *testing.T) {
	home, _ := jcodeHome(t, "")
	t.Setenv("PATH", t.TempDir()) // no jcode on PATH: only its folder tells (#1646)
	dir := filepath.Join(home, "elsewhere")
	t.Setenv("JCODE_HOME", dir)
	a := jcode(home)
	if a.Path != filepath.Join(dir, "config.toml") || a.Detected() {
		t.Fatalf("path %q detected %v", a.Path, a.Detected())
	}
	if err := a.Pick("model", "uni-switch/kimi/k2"); err != nil {
		t.Fatal(err)
	}
	if !a.Wired() || !a.Detected() {
		t.Fatalf("wired %v detected %v", a.Wired(), a.Detected())
	}
	writeFile(t, a.Path, "[provider]\ndefault_provider = \"uni-switch\"\ndefault_model = \"kimi/k2\"\n")
	if c := a.Check(); !strings.Contains(c, "is gone") {
		t.Fatalf("check: %q", c)
	}
}
