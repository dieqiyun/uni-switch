package agent

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tidwall/gjson"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// astrbotConfig is a cmd_config.json as AstrBot 4.28.2 writes it (json.dump,
// indent 2, ensure_ascii off, utf-8-sig) with the user's own DeepSeek source
// and one model of it — the shape its WebUI's buildModelProviderConfig makes
// — picked as the default chat model. The rest of the file is cut to a few
// of its keys.
const astrbotConfig = "\xef\xbb\xbf" + `{
  "config_version": 3,
  "provider_sources": [
    {
      "id": "deepseek",
      "provider": "deepseek",
      "type": "openai_chat_completion",
      "provider_type": "chat_completion",
      "enable": true,
      "key": [
        "sk-user"
      ],
      "api_base": "https://api.deepseek.com/v1",
      "timeout": 120,
      "proxy": "",
      "custom_headers": {}
    }
  ],
  "provider": [
    {
      "id": "deepseek/deepseek-chat",
      "enable": true,
      "provider_source_id": "deepseek",
      "model": "deepseek-chat",
      "modalities": [
        "text",
        "tool_use"
      ],
      "custom_extra_body": {},
      "max_context_tokens": 0
    }
  ],
  "provider_settings": {
    "enable": true,
    "wake_prefix": "",
    "web_search": false
  },
  "agent_runner": {
    "runner_type": "local",
    "config": {
      "model": {
        "provider_id": "deepseek/deepseek-chat",
        "fallback_provider_ids": [],
        "request_max_retries": 5
      }
    }
  },
  "dashboard": {
    "enable": true,
    "username": "astrbot",
    "port": 6185
  },
  "timezone": "Asia/Shanghai"
}`

// astrbotHome is a HOME with the AstrBot desktop app's ~/.astrbot holding
// body as its cmd_config.json, and uni-switch with a provider of two models.
func astrbotHome(t *testing.T, body string) (home, path string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("ASTRBOT_ROOT", "")
	if err := provider.Save(provider.Provider{ID: "kimi", Name: "Kimi", Key: "k",
		Chat: "https://api.moonshot.cn/v1", Models: []string{"k2", "k2-vision"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("kimi", "https://api.moonshot.cn/v1", []catalog.Model{
		{ID: "k2", Context: 262144},
		{ID: "k2-vision", Images: true},
	}); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(home, ".astrbot", "data", "cmd_config.json")
	if body != "" {
		writeFile(t, path, body)
	}
	return home, path
}

func astrbotRead(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b, astrbotBOM) {
		t.Fatalf("%s lost AstrBot's BOM:\n%.40q", path, b)
	}
	var m map[string]any
	if err := json.Unmarshal(bytes.TrimPrefix(b, astrbotBOM), &m); err != nil {
		t.Fatalf("%s is not JSON: %v\n%s", path, err, b)
	}
	return m
}

// astrbotByID is the entries of the list at k, by id.
func astrbotByID(t *testing.T, m map[string]any, k string) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, x := range m[k].([]any) {
		e := x.(map[string]any)
		out[e["id"].(string)] = e
	}
	return out
}

// astrbotReversed is r indented as AstrBot writes it, every object's keys in
// the reverse of their order: the same settings, in an order uni-switch doesn't
// write them in.
func astrbotReversed(r gjson.Result, in string) string {
	switch {
	case r.IsObject():
		var kv []string
		r.ForEach(func(k, v gjson.Result) bool {
			kv = append([]string{in + "  " + k.Raw + ": " + astrbotReversed(v, in+"  ")}, kv...)
			return true
		})
		if len(kv) == 0 {
			return "{}"
		}
		return "{\n" + strings.Join(kv, ",\n") + "\n" + in + "}"
	case r.IsArray():
		var vs []string
		for _, v := range r.Array() {
			vs = append(vs, in+"  "+astrbotReversed(v, in+"  "))
		}
		if len(vs) == 0 {
			return "[]"
		}
		return "[\n" + strings.Join(vs, ",\n") + "\n" + in + "]"
	}
	return r.Raw
}

// astrbotEdit changes AstrBot's settings as its WebUI does: the whole file
// written again from what it holds, keys in an order of its own.
func astrbotEdit(t *testing.T, path string, change func(m map[string]any)) {
	t.Helper()
	m := astrbotRead(t, path)
	change(m)
	var b bytes.Buffer
	b.Write(astrbotBOM)
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, b.String())
}

func astrbotPick(m map[string]any) any {
	return m["agent_runner"].(map[string]any)["config"].(map[string]any)["model"].(map[string]any)["provider_id"]
}

// Connecting AstrBot adds uni-switch's provider source and one model entry for
// each of uni-switch's models, typed as AstrBot's validator types them, and
// makes the one picked AstrBot's default chat model. The user's own source
// and model stay as they were, the file keeps its BOM, a copy of it is kept
// before uni-switch writes, and Disconnect gives back the file byte for byte.
func TestAstrBot(t *testing.T) {
	home, path := astrbotHome(t, astrbotConfig)
	a := astrbot(home)
	if a.Path != path || !a.Detected() {
		t.Fatalf("path %q, detected %v", a.Path, a.Detected())
	}
	f := a.Field("model")
	if got := f.Get(); got != "deepseek/deepseek-chat" || a.Wired() || a.Check() != "" {
		t.Fatalf("before: %q wired %v check %q", got, a.Wired(), a.Check())
	}
	var own, ours bool
	for _, o := range f.Options(a.Values()) {
		own = own || o.Value == "deepseek/deepseek-chat"
		ours = ours || o.Value == "uni-switch/kimi/k2"
	}
	if !own || !ours {
		t.Fatalf("options: own %v uni-switch %v", own, ours)
	}

	if err := a.Pick("model", "uni-switch/kimi/k2"); err != nil {
		t.Fatal(err)
	}
	m := astrbotRead(t, path)
	if got := astrbotPick(m); got != "uni-switch/kimi/k2" {
		t.Fatalf("default chat model %v", got)
	}
	if !a.Wired() || f.Get() != "uni-switch/kimi/k2" {
		t.Fatalf("wired %v, model %q", a.Wired(), f.Get())
	}
	if c := a.Check(); c != "" {
		t.Fatalf("check: %s", c)
	}
	sources := astrbotByID(t, m, "provider_sources")
	src := sources["uni-switch"]
	if src == nil || src["type"] != "openai_chat_completion" || src["provider_type"] != "chat_completion" ||
		!strings.HasSuffix(src["api_base"].(string), "/v1") || src["timeout"] != float64(120) {
		t.Fatalf("uni-switch's source: %v", src)
	}
	if k, _ := src["key"].([]any); len(k) != 1 || k[0] != agentKeyAt("astrbot", here(home).gw()) {
		t.Fatalf("uni-switch's key must be a list of the key naming AstrBot: %v", src["key"])
	}
	if !reflect.DeepEqual(sources["deepseek"]["key"], []any{"sk-user"}) {
		t.Fatalf("the user's source: %v", sources["deepseek"])
	}
	models := astrbotByID(t, m, "provider")
	k2, vision := models["uni-switch/kimi/k2"], models["uni-switch/kimi/k2-vision"]
	if k2 == nil || vision == nil || models["deepseek/deepseek-chat"] == nil {
		t.Fatalf("models: %v", models)
	}
	// k2 takes images only when uni-switch describes them to it
	k2mods := []any{"text", "tool_use"}
	if provider.Described != nil && provider.Described() {
		k2mods = []any{"text", "image", "tool_use"}
	}
	if k2["provider_source_id"] != "uni-switch" || k2["model"] != "kimi/k2" || k2["max_context_tokens"] != float64(262144) ||
		!reflect.DeepEqual(k2["modalities"], k2mods) || !reflect.DeepEqual(k2["custom_extra_body"], map[string]any{}) {
		t.Fatalf("k2: %v", k2)
	}
	if !reflect.DeepEqual(vision["modalities"], []any{"text", "image", "tool_use"}) || vision["max_context_tokens"] != float64(0) {
		t.Fatalf("k2-vision: %v", vision)
	}
	// the user's own entries are as they were, byte for byte, in front of
	// uni-switch's
	b, _ := os.ReadFile(path)
	for _, own := range []string{
		astrbotConfig[strings.Index(astrbotConfig, "    {\n      \"id\": \"deepseek\",") : strings.Index(astrbotConfig, "  ],\n  \"provider\": [")-1],
		astrbotConfig[strings.Index(astrbotConfig, "    {\n      \"id\": \"deepseek/deepseek-chat\"") : strings.Index(astrbotConfig, "  ],\n  \"provider_settings\"")-1],
	} {
		if !bytes.Contains(b, []byte(own+",\n    {\n      \"")) {
			t.Fatalf("the user's entry changed or moved:\n%s\nin\n%s", own, b)
		}
	}
	if bk, err := os.ReadFile(path + ".before-uni-switch"); err != nil || string(bk) != astrbotConfig {
		t.Fatalf("backup: %v\n%s", err, bk)
	}

	// switching between uni-switch's models keeps what AstrBot was on before
	// uni-switch, and leaves the backup as it was
	if err := a.Pick("model", "uni-switch/kimi/k2-vision"); err != nil {
		t.Fatal(err)
	}
	if bk, _ := os.ReadFile(path + ".before-uni-switch"); string(bk) != astrbotConfig {
		t.Fatalf("the backup was written over:\n%s", bk)
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != astrbotConfig {
		t.Fatalf("Disconnect left:\n%s", got)
	}
	if a.Wired() {
		t.Fatal("still wired")
	}
}

// What the user changes in uni-switch's entries in AstrBot's WebUI — a timeout,
// a proxy, a model switched off — is theirs: a catalog sync rewrites the
// list of models and the endpoint, not those. A sync that has nothing to
// change doesn't write, though AstrBot wrote the entries' keys in an order
// of its own.
func TestAstrBotSyncKeepsTheirs(t *testing.T) {
	home, path := astrbotHome(t, astrbotConfig)
	a := astrbot(home)
	if err := a.Pick("model", "uni-switch/kimi/k2"); err != nil {
		t.Fatal(err)
	}
	// as AstrBot's WebUI saves it: the whole file, from what it holds
	astrbotEdit(t, path, func(m map[string]any) {
		src := astrbotByID(t, m, "provider_sources")["uni-switch"]
		src["timeout"], src["proxy"] = 300, "http://127.0.0.1:7890"
		astrbotByID(t, m, "provider")["uni-switch/kimi/k2-vision"]["enable"] = false
	})
	// a model uni-switch lists no more leaves AstrBot's list; one it lists
	// now comes in
	if err := catalog.SaveLive("kimi", "https://api.moonshot.cn/v1", []catalog.Model{
		{ID: "k2", Context: 262144}, {ID: "k2-vision", Images: true}, {ID: "k3"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "kimi", Name: "Kimi", Key: "k",
		Chat: "https://api.moonshot.cn/v1", Models: []string{"k2", "k2-vision", "k3"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	m := astrbotRead(t, path)
	src := astrbotByID(t, m, "provider_sources")["uni-switch"]
	if src["timeout"] != float64(300) || src["proxy"] != "http://127.0.0.1:7890" {
		t.Fatalf("the user's timeout and proxy were written over: %v", src)
	}
	models := astrbotByID(t, m, "provider")
	if models["uni-switch/kimi/k2-vision"]["enable"] != false {
		t.Fatalf("a model the user switched off came back on: %v", models["uni-switch/kimi/k2-vision"])
	}
	if models["uni-switch/kimi/k3"] == nil {
		t.Fatalf("a model uni-switch lists now isn't in AstrBot's list: %v", models)
	}

	// AstrBot's WebUI writes every key back in an order of its own; the
	// same entries again are no change
	b, _ := os.ReadFile(path)
	saved := append(append([]byte(nil), astrbotBOM...), astrbotReversed(gjson.ParseBytes(bytes.TrimPrefix(b, astrbotBOM)), "")...)
	writeFile(t, path, string(saved))
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, saved) {
		t.Fatalf("a sync with nothing to change wrote the file:\n%s", got)
	}
}

// Picking no model of uni-switch's in the picker puts AstrBot back on the one it
// was on before uni-switch, and takes uni-switch's source and models out.
func TestAstrBotNoModelPutsTheirsBack(t *testing.T) {
	home, path := astrbotHome(t, astrbotConfig)
	a := astrbot(home)
	if err := a.Pick("model", "uni-switch/kimi/k2"); err != nil {
		t.Fatal(err)
	}
	if err := a.Field("model").Set(""); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != astrbotConfig {
		t.Fatalf("left:\n%s", got)
	}
}

// A default chat model the user picks in AstrBot while uni-switch is wired is
// theirs: Disconnect takes uni-switch's source and models out and leaves the
// pick as it is.
func TestAstrBotTheirPickStands(t *testing.T) {
	home, path := astrbotHome(t, astrbotConfig)
	a := astrbot(home)
	if err := a.Pick("model", "uni-switch/kimi/k2"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	writeFile(t, path, strings.Replace(string(b), `"provider_id": "uni-switch/kimi/k2"`, `"provider_id": "openai/gpt-5"`, 1))
	if !a.Wired() {
		t.Fatal("uni-switch's models are still in AstrBot's list: it is connected")
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	m := astrbotRead(t, path)
	if got := astrbotPick(m); got != "openai/gpt-5" {
		t.Fatalf("the user's pick became %v", got)
	}
	if _, ok := astrbotByID(t, m, "provider_sources")["uni-switch"]; ok {
		t.Fatal("uni-switch's source stayed")
	}
	for id := range astrbotByID(t, m, "provider") {
		if strings.HasPrefix(id, "uni-switch/") {
			t.Fatalf("uni-switch's model %s stayed", id)
		}
	}
}

// Before 4.28 AstrBot named its default chat model in
// provider_settings.default_provider_id, with no agent_runner; uni-switch sets
// that one there, and puts it back.
func TestAstrBotBeforeAgentRunner(t *testing.T) {
	old := strings.Replace(astrbotConfig, `"web_search": false`, `"web_search": false,
    "default_provider_id": "deepseek/deepseek-chat"`, 1)
	old = old[:strings.Index(old, `  "agent_runner"`)] + old[strings.Index(old, `  "dashboard"`):]
	home, path := astrbotHome(t, old)
	a := astrbot(home)
	if got := a.Field("model").Get(); got != "deepseek/deepseek-chat" {
		t.Fatalf("model %q", got)
	}
	if err := a.Pick("model", "uni-switch/kimi/k2"); err != nil {
		t.Fatal(err)
	}
	m := astrbotRead(t, path)
	if _, ok := m["agent_runner"]; ok {
		t.Fatal("uni-switch made an agent_runner of its own")
	}
	if got := m["provider_settings"].(map[string]any)["default_provider_id"]; got != "uni-switch/kimi/k2" {
		t.Fatalf("default_provider_id %v", got)
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != old {
		t.Fatalf("Disconnect left:\n%s", got)
	}
}

// uni-switch writes no AstrBot settings of its own: with no cmd_config.json yet
// (AstrBot never started) or one from before provider_sources (older than
// 4.10), it says what to do and leaves the folder as it is.
func TestAstrBotRefusesWithoutItsSettings(t *testing.T) {
	home, path := astrbotHome(t, "")
	os.MkdirAll(filepath.Join(home, ".astrbot"), 0o755)
	a := astrbot(home)
	if err := a.Pick("model", "uni-switch/kimi/k2"); err == nil || !strings.Contains(err.Error(), "start AstrBot once") {
		t.Fatalf("no settings: %v", err)
	}
	if isFile(path) {
		t.Fatal("uni-switch made AstrBot's settings")
	}
	old := `{"provider": [{"id": "openai", "type": "openai_chat_completion", "key": ["sk"]}], "provider_settings": {"default_provider_id": "openai"}}`
	writeFile(t, path, old)
	if err := astrbot(home).Pick("model", "uni-switch/kimi/k2"); err == nil || !strings.Contains(err.Error(), "4.10") {
		t.Fatalf("before provider_sources: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != old {
		t.Fatalf("the file changed:\n%s", got)
	}
}

// AstrBot's root: ASTRBOT_ROOT, the desktop app's ~/.astrbot, or a folder
// `astrbot init` was run in that uni-switch can know of.
func TestAstrBotRoot(t *testing.T) {
	home, _ := astrbotHome(t, "")
	at := here(home)
	if got := astrbotRoot(at); got != filepath.Join(home, ".astrbot") {
		t.Fatalf("nothing there: %s", got)
	}
	writeFile(t, filepath.Join(home, "AstrBot", ".astrbot"), "")
	if got := astrbotRoot(at); !sameDir(got, filepath.Join(home, "AstrBot")) {
		t.Fatalf("~/AstrBot: %s", got)
	}
	writeFile(t, filepath.Join(home, ".astrbot"), "")
	if got := astrbotRoot(at); got != home {
		t.Fatalf("init in the home: %s", got)
	}
	t.Setenv("ASTRBOT_ROOT", filepath.Join(home, "bots", "mine"))
	if got := astrbotRoot(here(home)); got != filepath.Join(home, "bots", "mine") {
		t.Fatalf("ASTRBOT_ROOT: %s", got)
	}
}

// sameDir says a and b are one folder, on a file system that may not tell
// names' cases apart (macOS's, where ~/astrbot is ~/AstrBot).
func sameDir(a, b string) bool {
	sa, err1 := os.Stat(a)
	sb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(sa, sb)
}
