package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// grokAppConfig is agent-home/config.toml as Grok App 0.2.39 writes it
// after a DeepSeek channel is added on its Settings › Providers page and
// made the route (upsert_custom_provider, set_default_model_id), with the
// [models] max_retries it pins itself and a comment the user added by hand.
const grokAppConfig = `[models]
default = "deepseek"
max_retries = 3

# my own channel
[model.deepseek]
model = "deepseek-v4-flash"
base_url = "https://api.deepseek.com/v1"
name = "DeepSeek"
api_key = "sk-test"
api_backend = "chat_completions"
app_provider_mode = "generic"
app_models = "[{\"id\":\"deepseek-v4-flash\",\"name\":\"Flash\"}]"
app_efforts = "[]"
supports_reasoning_effort = false
context_window = 1000000
`

// grokAppHome is a HOME with Grok App's data folder under GROK_APP_HOME,
// its agent-home/config.toml holding body, and uni-switch with a provider of
// two models, one of them with effort levels.
func grokAppHome(t *testing.T, body string) (home, path string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("GROK_APP_HOME", filepath.Join(home, "grok-app-data"))
	// images only where the model takes them itself, not where uni-switch's
	// vision model describes them
	was := provider.Described
	provider.Described = func() bool { return false }
	t.Cleanup(func() { provider.Described = was })
	if err := provider.Save(provider.Provider{ID: "kimi", Name: "Kimi", Key: "k",
		Chat: "https://api.moonshot.cn/v1", Models: []string{"k2", "k2-vision"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("kimi", "https://api.moonshot.cn/v1", []catalog.Model{
		{ID: "k2", Context: 262144, Efforts: []string{"none", "low", "medium", "high"}},
		{ID: "k2-vision", Images: true},
	}); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(home, "grok-app-data", "agent-home", "config.toml")
	if body != "" {
		writeFile(t, path, body)
	} else if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	return home, path
}

// grokAppSections reads config.toml the way Grok App itself does
// (providers.rs parse_model_sections and unquote): line by line, a
// [model.<id>] header starting a section, each `key = value` taken with a
// quoted value decoded as a JSON string — so what uni-switch writes has to be
// what that reader takes, not only valid TOML.
func grokAppSections(t *testing.T, path string) map[string]map[string]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	unquote := func(v string) string {
		v = strings.TrimSpace(v)
		if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			var s string
			if json.Unmarshal([]byte(v), &s) == nil {
				return s
			}
			return v[1 : len(v)-1]
		}
		return v
	}
	out := map[string]map[string]string{}
	var cur map[string]string
	for _, line := range strings.Split(string(b), "\n") {
		l := strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(l, "[model."); ok && strings.HasSuffix(rest, "]") {
			cur = map[string]string{}
			out[unquote(strings.TrimSuffix(rest, "]"))] = cur
			continue
		}
		if strings.HasPrefix(l, "[") {
			cur = nil
			continue
		}
		if cur == nil || l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if k, v, ok := strings.Cut(l, "="); ok {
			cur[strings.TrimSpace(k)] = unquote(v)
		}
	}
	return out
}

// Connecting Grok App adds uni-switch as one of its provider channels — the
// gateway, with a key naming Grok App, and every one of uni-switch's models in
// the App's own app_models form — and leaves the route alone. The App's own
// channel stays in the file, and Disconnect gives the file back byte for
// byte.
func TestGrokApp(t *testing.T) {
	home, path := grokAppHome(t, grokAppConfig)
	a := grokApp(home)
	if a.Path != path || !a.Detected() {
		t.Fatalf("path %q, detected %v", a.Path, a.Detected())
	}
	f := a.Field("model")
	if got := f.Get(); got != "deepseek" || a.Wired() || a.Check() != "" {
		t.Fatalf("before: %q wired %v check %q", got, a.Wired(), a.Check())
	}
	opts := map[string]Option{}
	for _, o := range f.Options(a.Values()) {
		opts[o.Value] = o
	}
	if opts["deepseek"].Direct != "api.deepseek.com" || opts["deepseek"].Label != "DeepSeek" ||
		opts["grok"].Value == "" || opts["uni-switch/kimi/k2"].Value == "" {
		t.Fatalf("options: %v", opts)
	}

	if err := a.Pick("model", "uni-switch/kimi/k2"); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	b, _ := os.ReadFile(path)
	if err := toml.Unmarshal(b, &m); err != nil {
		t.Fatalf("not TOML (Grok Build reads it as TOML): %v\n%s", err, b)
	}
	if d := m["models"].(map[string]any); d["default"] != "deepseek" || d["max_retries"] != int64(3) {
		t.Fatalf("[models] route changed: %v", d)
	}
	ch := m["model"].(map[string]any)["uni-switch"].(map[string]any)
	// Grok Build reads these two bare: a quoted context window is what the
	// App heals as broken (#538 of its own), and the effort gate a bool
	if ch["context_window"] != int64(262144) || ch["supports_reasoning_effort"] != true {
		t.Fatalf("bare keys: %v", ch)
	}
	if !a.Wired() || f.Get() != "uni-switch/kimi/k2" {
		t.Fatalf("wired %v, model %q", a.Wired(), f.Get())
	}
	if c := a.Check(); c != "" {
		t.Fatalf("check: %s", c)
	}

	secs := grokAppSections(t, path)
	s := secs["uni-switch"]
	if s["model"] != "kimi/k2" || s["base_url"] != here(home).v1() || s["name"] != "uni-switch" ||
		s["api_key"] != agentKeyAt("grok-app", here(home).gw()) || s["api_backend"] != "chat_completions" ||
		s["app_provider_mode"] != "generic" || s["context_window"] != "262144" || s["supports_reasoning_effort"] != "true" {
		t.Fatalf("uni-switch's channel as the App reads it: %v", s)
	}
	if _, ok := s["app_supports_vision"]; ok {
		t.Fatalf("k2 takes no images: %v", s)
	}
	// app_models decodes as the App's Vec<ProviderModelEntry> (camelCase)
	var models []struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		ContextWindow  *int   `json:"contextWindow"`
		SupportsVision *bool  `json:"supportsVision"`
		Efforts        []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			IsDefault bool   `json:"isDefault"`
		} `json:"efforts"`
	}
	if err := json.Unmarshal([]byte(s["app_models"]), &models); err != nil || len(models) != 2 {
		t.Fatalf("app_models %q: %v", s["app_models"], err)
	}
	k2, vision := models[0], models[1]
	if k2.ID != "kimi/k2" || k2.ContextWindow == nil || *k2.ContextWindow != 262144 || k2.SupportsVision != nil {
		t.Fatalf("k2: %+v", k2)
	}
	var levels, defs []string
	for _, e := range k2.Efforts {
		levels = append(levels, e.ID)
		if e.IsDefault {
			defs = append(defs, e.ID)
		}
	}
	// no "none" (Grok Build's channel effort is a level), one default
	if !reflect.DeepEqual(levels, []string{"low", "medium", "high"}) || !reflect.DeepEqual(defs, []string{"medium"}) {
		t.Fatalf("k2's efforts %v, default %v", levels, defs)
	}
	if vision.ID != "kimi/k2-vision" || vision.SupportsVision == nil || !*vision.SupportsVision || len(vision.Efforts) != 0 {
		t.Fatalf("k2-vision: %+v", vision)
	}
	if !strings.Contains(s["app_efforts"], `"id":"medium","name":"medium","isDefault":true`) {
		t.Fatalf("app_efforts: %s", s["app_efforts"])
	}
	if !reflect.DeepEqual(secs["deepseek"], grokAppSectionsOf(t, grokAppConfig)["deepseek"]) {
		t.Fatalf("the user's own channel changed: %v", secs["deepseek"])
	}
	if !strings.Contains(string(b), "# my own channel\n[model.deepseek]") {
		t.Fatalf("the user's comment went:\n%s", b)
	}

	// another of uni-switch's models: the channel's extras are that one's
	if err := a.Pick("model", "uni-switch/kimi/k2-vision"); err != nil {
		t.Fatal(err)
	}
	s = grokAppSections(t, path)["uni-switch"]
	if s["model"] != "kimi/k2-vision" || s["app_supports_vision"] != "true" || s["supports_reasoning_effort"] != "false" ||
		s["app_efforts"] != "[]" {
		t.Fatalf("k2-vision's channel: %v", s)
	}
	if _, ok := s["context_window"]; ok {
		t.Fatalf("k2-vision has no context window of its own: %v", s)
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != grokAppConfig {
		t.Fatalf("Disconnect left:\n%s", got)
	}
	if a.Wired() {
		t.Fatal("still wired")
	}
}

func grokAppSectionsOf(t *testing.T, body string) map[string]map[string]string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	writeFile(t, p, body)
	return grokAppSections(t, p)
}

// A model picked in the App's own composer since, and an effort the user
// made the default on its Providers page, are theirs: a sync keeps both,
// and a sync with nothing changed writes nothing.
func TestGrokAppSyncKeepsTheirs(t *testing.T) {
	home, path := grokAppHome(t, grokAppConfig)
	a := grokApp(home)
	if err := a.Pick("model", "uni-switch/kimi/k2"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	// the App's composer switching to k2-vision rewrites model =, and the
	// user marked high k2's default
	edited := strings.Replace(string(b), `model = "kimi/k2"`, `model = "kimi/k2-vision"`, 1)
	edited = strings.Replace(edited, `{\"id\":\"medium\",\"name\":\"medium\",\"isDefault\":true},{\"id\":\"high\",\"name\":\"high\",\"isDefault\":false}`,
		`{\"id\":\"medium\",\"name\":\"medium\",\"isDefault\":false},{\"id\":\"high\",\"name\":\"high\",\"isDefault\":true}`, 1)
	if edited == string(b) || !strings.Contains(edited, `{\"id\":\"high\",\"name\":\"high\",\"isDefault\":true}]}`) {
		t.Fatalf("fixture edit didn't take:\n%s", edited)
	}
	writeFile(t, path, edited)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	s := grokAppSections(t, path)["uni-switch"]
	if s["model"] != "kimi/k2-vision" || s["app_supports_vision"] != "true" {
		t.Fatalf("the App's pick went: %v", s)
	}
	if !strings.Contains(s["app_models"], `{"id":"high","name":"high","isDefault":true}`) {
		t.Fatalf("the user's default effort went: %s", s["app_models"])
	}
	before, _ := os.ReadFile(path)
	st, _ := os.Stat(path)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	st2, _ := os.Stat(path)
	if string(after) != string(before) || !st2.ModTime().Equal(st.ModTime()) {
		t.Fatalf("a sync with nothing changed wrote:\n%s", after)
	}
}

// Picking the App's own route in uni-switch does not take the App over and does
// not remove the custom provider. The route stays the one the App had.
func TestGrokAppOwnRouteStays(t *testing.T) {
	home, path := grokAppHome(t, grokAppConfig)
	a := grokApp(home)
	if err := a.Pick("model", "uni-switch/kimi/k2"); err != nil {
		t.Fatal(err)
	}
	def, _, rerr := grokAppRead(path)
	if rerr != nil || def != "deepseek" {
		t.Fatalf("connecting moved the route: %q err %v", def, rerr)
	}
	// one of the App's own routes the picker offers is taken: it is the
	// route, and uni-switch's channel goes, as picking another provider leaves
	// uni-switch on every other agent
	if err := a.Pick("model", "grok"); err != nil {
		t.Fatalf("picking the official route: %v", err)
	}
	secs := grokAppSections(t, path)
	if _, ok := secs["uni-switch"]; ok || a.Field("model").Get() != "grok" {
		t.Fatalf("uni-switch's channel stayed: model %q\n%v", a.Field("model").Get(), secs["uni-switch"])
	}
	def, _, rerr = grokAppRead(path)
	if rerr != nil || def != "grok" {
		t.Fatalf("route %q err %v", def, rerr)
	}
	if secs["deepseek"]["api_key"] != "sk-test" {
		t.Fatalf("the user's channel went: %v", secs["deepseek"])
	}
}

// Disconnect puts back the route the App was on before Connect through the
// model field (connect.was) when unwire left none: a route Use set to
// uni-switch with nothing stashed to put back. Set refused an own route, so
// Disconnect failed half done.
func TestGrokAppDisconnectPutsTheRouteBack(t *testing.T) {
	home, path := grokAppHome(t, grokAppConfig)
	a := grokApp(home)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	if err := edit.SetTOMLKey(path, "models", "default", uniSwitchID); err != nil { // Use on uni-switch
		t.Fatal(err)
	}
	forget("grok-app:" + path + ":default")
	if err := a.Disconnect(); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	def, ch, err := grokAppRead(path)
	if err != nil || def != "deepseek" || ch != nil {
		t.Fatalf("after disconnect: route %q channel %v err %v", def, ch, err)
	}
}

// A sync with nothing of uni-switch's in the App writes nothing, not even to
// uni-switch's own stash; one whose route is uni-switch with the channel gone is
// told, since the App reaches nothing.
func TestGrokAppSyncUnconnected(t *testing.T) {
	home, path := grokAppHome(t, grokAppConfig)
	a := grokApp(home)
	before := mustRead(t, path)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	st := stashLoad()
	if mustRead(t, path) != before || st["grok-app:"+path+":default"] != "" || st[grokAppReleased] != "" {
		t.Fatalf("an unconnected sync wrote: %v", st)
	}
	if c := a.Check(); c != "" {
		t.Fatalf("check: %q", c)
	}
	stash(map[string]string{grokAppReleased: "1"}) // a Use after the one put back
	if err := os.WriteFile(path, []byte(strings.Replace(before, `default = "deepseek"`, `default = "uni-switch"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if c := a.Check(); !strings.Contains(c, "no longer reaches uni-switch") {
		t.Fatalf("route uni-switch without the channel: check %q", c)
	}
}

// An older uni-switch that set the route to itself is put back once. A Use the
// user presses after that is the App's choice and stays.
func TestGrokAppReleasesATakeover(t *testing.T) {
	home, path := grokAppHome(t, grokAppConfig)
	a := grokApp(home)
	stash(map[string]string{"grok-app:" + path + ":default": "deepseek"})
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(b), `default = "deepseek"`, `default = "uni-switch"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	def, _, err := grokAppRead(path)
	if err != nil || def != "deepseek" {
		t.Fatalf("after sync: route %q err %v", def, err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(mustRead(t, path)), `default = "deepseek"`, `default = "uni-switch"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	def, _, err = grokAppRead(path)
	if err != nil || def != "uni-switch" {
		t.Fatalf("Use was undone: %q err %v", def, err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// With no channel of the user's and no route set (a fresh install, Grok's
// own sign-in), Disconnect leaves no route either: the App is back on
// Grok's official one.
func TestGrokAppFresh(t *testing.T) {
	home, path := grokAppHome(t, "")
	a := grokApp(home)
	if !a.Detected() {
		t.Fatal("not detected")
	}
	if err := a.Pick("model", "uni-switch/kimi/k2-vision"); err != nil {
		t.Fatal(err)
	}
	if s := grokAppSections(t, path)["uni-switch"]; s["model"] != "kimi/k2-vision" {
		t.Fatalf("channel: %v", s)
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	def, ch, err := grokAppRead(path)
	if err != nil || def != "" || ch != nil {
		t.Fatalf("after Disconnect: default %q channel %v err %v", def, ch, err)
	}
}

// The data folder is where the App's directories crate puts it on each
// system, else one of its fallbacks when that is the one there.
func TestGrokAppDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GROK_APP_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("APPDATA", filepath.Join(home, "Roaming"))
	for goos, want := range map[string]string{
		"darwin":  filepath.Join(home, "Library", "Application Support", "com.grokapp.grok-app"),
		"linux":   filepath.Join(home, ".local", "share", "grok-app"),
		"windows": filepath.Join(home, "Roaming", "grokapp", "grok-app", "data"),
	} {
		if got := grokAppDir(home, goos); got != want {
			t.Errorf("%s: %s, want %s", goos, got, want)
		}
	}
	if err := os.MkdirAll(filepath.Join(home, ".grok-app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := grokAppDir(home, "linux"); got != filepath.Join(home, ".grok-app") {
		t.Errorf("linux fallback: %s", got)
	}
	t.Setenv("GROK_APP_HOME", filepath.Join(home, "x"))
	if got := grokAppDir(home, "darwin"); got != filepath.Join(home, "x") {
		t.Errorf("GROK_APP_HOME: %s", got)
	}
}
