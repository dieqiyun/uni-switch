package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/tidwall/gjson"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// qwenpawConfig is a config.json as QwenPaw 2.2.2 writes it (config.py's
// Config, cut to its agents and a few other keys): the default agent, a
// second one of the user's on QwenPaw's own backend, one on an ACP backend
// and one switched off.
const qwenpawConfig = `{
  "agents": {
    "active_agent": "default",
    "agent_order": [
      "default",
      "writer",
      "coder",
      "old"
    ],
    "profiles": {
      "default": {
        "id": "default",
        "workspace_dir": "HOME/.qwenpaw/workspaces/default",
        "enabled": true,
        "pinned": false
      },
      "writer": {
        "id": "writer",
        "workspace_dir": "HOME/.qwenpaw/workspaces/writer",
        "enabled": true,
        "pinned": false
      },
      "coder": {
        "id": "coder",
        "workspace_dir": "HOME/.qwenpaw/workspaces/coder",
        "enabled": true,
        "pinned": false
      },
      "old": {
        "id": "old",
        "workspace_dir": "HOME/.qwenpaw/workspaces/old",
        "enabled": false,
        "pinned": false
      }
    }
  },
  "acp": {
    "agents": {}
  },
  "show_tool_details": true
}
`

// qwenpawAgentJSON is an agent.json (AgentProfileConfig) on the user's own
// DeepSeek provider, cut to the keys around its model.
func qwenpawAgentJSON(id, name, backend string) string {
	return `{
  "id": "` + id + `",
  "name": "` + name + `",
  "description": "",
  "workspace_dir": "",
  "backend": "` + backend + `",
  "active_model": {
    "provider_id": "deepseek",
    "model": "deepseek-chat"
  },
  "fallback_models": [
    {
      "provider_id": "deepseek",
      "model": "deepseek-reasoner"
    }
  ],
  "language": "zh",
  "running": {
    "max_iters": 100
  }
}
`
}

// qwenpawDeepseek is a built-in provider file of QwenPaw's with the user's
// key (encrypted, as QwenPaw keeps it).
const qwenpawDeepseek = `{
  "id": "deepseek",
  "name": "DeepSeek",
  "base_url": "https://api.deepseek.com",
  "api_key": "ENC:gAAAAABuser",
  "chat_model": "OpenAIChatModel",
  "models": [
    {"id": "deepseek-chat", "name": "DeepSeek Chat"},
    {"id": "deepseek-reasoner", "name": "DeepSeek Reasoner"}
  ],
  "extra_models": [],
  "is_custom": false,
  "removed_model_ids": ["deepseek-reasoner"],
  "snapshot_schema_version": 2
}
`

// qwenpawFixture puts QwenPaw's files, as it writes them at its first
// start, under home: the default agent, the writer, the ACP coder, and the
// user's DeepSeek provider. It returns the working folder.
func qwenpawFixture(t *testing.T, home string) string {
	t.Helper()
	work := filepath.Join(home, ".qwenpaw")
	writeFile(t, filepath.Join(work, "config.json"), strings.ReplaceAll(qwenpawConfig, "HOME", filepath.ToSlash(home)))
	writeFile(t, filepath.Join(work, "workspaces", "default", "agent.json"), qwenpawAgentJSON("default", "Default Agent", "qwenpaw"))
	writeFile(t, filepath.Join(work, "workspaces", "writer", "agent.json"), qwenpawAgentJSON("writer", "Writer", "qwenpaw"))
	writeFile(t, filepath.Join(work, "workspaces", "coder", "agent.json"), qwenpawAgentJSON("coder", "Coder", "claude_code"))
	writeFile(t, filepath.Join(work, "workspaces", "old", "agent.json"), qwenpawAgentJSON("old", "Old", "qwenpaw"))
	writeFile(t, filepath.Join(work+".secret", "providers", "builtin", "deepseek.json"), qwenpawDeepseek)
	return work
}

// qwenpawHome is a sandbox HOME with QwenPaw's files and uni-switch with a
// provider of two models.
func qwenpawHome(t *testing.T) (home, work string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("QWENPAW_WORKING_DIR", "")
	t.Setenv("QWENPAW_SECRET_DIR", "")
	noKeychain(t)
	if err := provider.Save(provider.Provider{ID: "kimi", Name: "Kimi", Key: "k",
		Chat: "https://api.moonshot.cn/v1", Models: []string{"k2", "k2-vision"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("kimi", "https://api.moonshot.cn/v1", []catalog.Model{
		{ID: "k2", Context: 262144, Output: 16384, Efforts: []string{"none", "low", "medium", "high", "max", "ultra"}},
		{ID: "k2-vision", Images: true},
	}); err != nil {
		t.Fatal(err)
	}
	return home, qwenpawFixture(t, home)
}

func qwenpawJSON(t *testing.T, path string) gjson.Result {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(b) {
		t.Fatalf("%s is not JSON:\n%s", path, b)
	}
	return gjson.ParseBytes(b)
}

func qwenpawFind(t *testing.T, id string) *Agent {
	t.Helper()
	a, err := Find(id)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// Connecting QwenPaw writes uni-switch's provider where QwenPaw keeps its own
// (an OpenAI Chat provider at the gateway, with a key naming QwenPaw and
// uni-switch's models) and sets the default agent's active_model to one of
// uni-switch's; Disconnect puts back the model it was on, byte for byte, and
// takes the provider out.
func TestQwenPawConnect(t *testing.T) {
	_, work := qwenpawHome(t)
	path := filepath.Join(work, "workspaces", "default", "agent.json")
	prov := filepath.Join(work+".secret", "providers", "custom", "uni-switch.json")
	before := readFile(path)
	a := qwenpawFind(t, "qwenpaw")
	if a.Path != path {
		t.Fatalf("path %s, want %s", a.Path, path)
	}
	if v := a.Fields[0].Get(); v != "deepseek/deepseek-chat" {
		t.Fatalf("own model reads %q", v)
	}
	if a.Wired() {
		t.Fatal("wired on its own DeepSeek")
	}
	var values []string
	for _, o := range a.Fields[0].Options(a.Values()) {
		values = append(values, o.Value)
	}
	for _, want := range []string{"deepseek/deepseek-chat", "uni-switch/kimi/k2", "uni-switch/kimi/k2-vision"} {
		if !slices.Contains(values, want) {
			t.Errorf("options %v lack %s", values, want)
		}
	}
	// removed_model_ids are gone from QwenPaw's own list
	if slices.Contains(values, "deepseek/deepseek-reasoner") {
		t.Errorf("a model the user removed is offered: %v", values)
	}
	if err := a.Apply("model", "uni-switch/kimi/k2"); err != nil {
		t.Fatal(err)
	}
	j := qwenpawJSON(t, path)
	if j.Get("active_model.provider_id").String() != "uni-switch" || j.Get("active_model.model").String() != "kimi/k2" {
		t.Fatalf("active_model: %s", j.Get("active_model").Raw)
	}
	for _, k := range []string{"fallback_models.0.model", "language", "running.max_iters", "name"} {
		if !j.Get(k).Exists() {
			t.Errorf("agent.json lost %s:\n%s", k, j.Raw)
		}
	}
	p := qwenpawJSON(t, prov)
	for k, want := range map[string]string{"id": "uni-switch", "name": "uni-switch", "base_url": gatewayV1(),
		"api_key": agentKeyAt("qwenpaw", gatewayV1()), "chat_model": "OpenAIChatModel", "is_custom": "true", "enabled": "true"} {
		if got := p.Get(k).String(); got != want {
			t.Errorf("provider %s = %q, want %q", k, got, want)
		}
	}
	k2 := p.Get(`extra_models.#(id=="kimi/k2")`)
	if k2.Get("max_input_length").Int() != 262144 || k2.Get("max_output_length").Int() != 16384 ||
		!k2.Get("supports_tool_calling").Bool() || k2.Get("source").String() != "user" {
		t.Errorf("kimi/k2: %s", k2.Raw)
	}
	if v := p.Get(`extra_models.#(id=="kimi/k2-vision")`); !v.Get("supports_image").Bool() || v.Get("max_input_length").Exists() {
		t.Errorf("kimi/k2-vision: %s", v.Raw)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(prov); st.Mode().Perm()&0o077 != 0 {
			t.Errorf("provider file mode %v, want the user's alone", st.Mode().Perm())
		}
	}
	if !a.Wired() {
		t.Fatalf("not wired: %v", a.Values())
	}
	if c := a.Check(); c != "" {
		t.Fatalf("check right after a set: %s", c)
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(path); got != before {
		t.Errorf("Disconnect left\n%s\nwant\n%s", got, before)
	}
	if isFile(prov) {
		t.Error("uni-switch's provider is still in QwenPaw after Disconnect")
	}
	if a.Wired() {
		t.Fatal("wired after Disconnect")
	}
}

// Each of QwenPaw's agents on its own backend is a row of its own, named
// as QwenPaw names it; one on an ACP backend, or switched off, isn't.
// uni-switch's provider stays while any agent still names it.
func TestQwenPawAgentsAreRows(t *testing.T) {
	_, work := qwenpawHome(t)
	prov := filepath.Join(work+".secret", "providers", "custom", "uni-switch.json")
	var ids []string
	for _, a := range All() {
		if strings.HasPrefix(a.ID, "qwenpaw") {
			ids = append(ids, a.ID+"="+a.Name)
		}
	}
	if want := []string{"qwenpaw=QwenPaw", "qwenpaw#writer=QwenPaw · Writer"}; !slices.Equal(ids, want) {
		t.Fatalf("rows %v, want %v", ids, want)
	}
	def, writer := qwenpawFind(t, "qwenpaw"), qwenpawFind(t, "qwenpaw#writer")
	if writer.Path != filepath.Join(work, "workspaces", "writer", "agent.json") {
		t.Fatalf("writer's path: %s", writer.Path)
	}
	if err := def.Apply("model", "uni-switch/kimi/k2"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Apply("model", "uni-switch/kimi/k2-vision"); err != nil {
		t.Fatal(err)
	}
	if def.Fields[0].Get() != "uni-switch/kimi/k2" || writer.Fields[0].Get() != "uni-switch/kimi/k2-vision" {
		t.Fatalf("default %q, writer %q", def.Fields[0].Get(), writer.Fields[0].Get())
	}
	if err := writer.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if writer.Fields[0].Get() != "deepseek/deepseek-chat" {
		t.Fatalf("writer back on %q", writer.Fields[0].Get())
	}
	if !isFile(prov) {
		t.Fatal("the writer's Disconnect took the provider the default agent is on")
	}
	if err := def.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if isFile(prov) {
		t.Fatal("the provider is left with no agent on it")
	}
	// the ACP agent is refused, not rewritten
	coder := qwenpawAt(here(os.Getenv("HOME")), work, qwenpawProfile{"coder", filepath.Join(work, "workspaces", "coder")})
	before := readFile(coder.Path)
	if err := coder.Apply("model", "uni-switch/kimi/k2"); err == nil || !strings.Contains(err.Error(), "claude_code") {
		t.Fatalf("an ACP agent took uni-switch's model: %v", err)
	}
	if readFile(coder.Path) != before {
		t.Fatal("the ACP agent's agent.json was written")
	}
}

// QwenPaw encrypts the key uni-switch wrote at its next start: a sync keeps
// it while it is the key uni-switch wrote, and keeps what the user changed in
// QwenPaw's console (headers, a model's window they set), while a new key
// is written again in plain text.
func TestQwenPawSyncKeepsWhatQwenPawChanged(t *testing.T) {
	_, work := qwenpawHome(t)
	prov := filepath.Join(work+".secret", "providers", "custom", "uni-switch.json")
	a := qwenpawFind(t, "qwenpaw")
	if err := a.Apply("model", "uni-switch/kimi/k2"); err != nil {
		t.Fatal(err)
	}
	// as QwenPaw saves it after its start and an edit in its console
	var m map[string]any
	json.Unmarshal([]byte(readFile(prov)), &m)
	m["api_key"] = "ENC:gAAAAABuni-switch"
	m["custom_headers"] = map[string]any{"X-Team": "a"}
	for _, e := range m["extra_models"].([]any) {
		e := e.(map[string]any)
		if e["id"] == "kimi/k2" {
			e["max_input_length"] = 50000
			e["config_overrides"] = []any{"max_input_length"}
			e["generate_kwargs"] = map[string]any{"temperature": 0.3}
		}
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	writeFile(t, prov, string(b))
	if c := a.Check(); c != "" {
		t.Fatalf("check on an encrypted key: %s", c)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	p := qwenpawJSON(t, prov)
	if p.Get("api_key").String() != "ENC:gAAAAABuni-switch" {
		t.Errorf("the encrypted key was written over: %s", p.Get("api_key"))
	}
	if p.Get("custom_headers.X-Team").String() != "a" {
		t.Errorf("headers lost: %s", p.Raw)
	}
	k2 := p.Get(`extra_models.#(id=="kimi/k2")`)
	if k2.Get("max_input_length").Int() != 50000 || k2.Get("generate_kwargs.temperature").Float() != 0.3 {
		t.Errorf("the user's settings of kimi/k2 lost: %s", k2.Raw)
	}
	// another key than the one uni-switch wrote: written again
	stash(map[string]string{"qwenpaw.key": "an-older-key"})
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := qwenpawJSON(t, prov).Get("api_key").String(); got != agentKeyAt("qwenpaw", gatewayV1()) {
		t.Errorf("a changed key isn't written: %q", got)
	}
}

// Check says what took QwenPaw off uni-switch: the provider's address
// changed, the provider switched off or removed, the agent moved to an
// ACP backend.
func TestQwenPawCheck(t *testing.T) {
	_, work := qwenpawHome(t)
	prov := filepath.Join(work+".secret", "providers", "custom", "uni-switch.json")
	a := qwenpawFind(t, "qwenpaw")
	if err := a.Apply("model", "uni-switch/kimi/k2"); err != nil {
		t.Fatal(err)
	}
	good := readFile(prov)
	for name, c := range map[string]struct {
		mend func()
		want string
	}{
		"address": {func() { writeFile(t, prov, strings.ReplaceAll(good, gatewayV1(), "http://127.0.0.1:9/v1")) }, "base_url"},
		"off":     {func() { writeFile(t, prov, strings.Replace(good, `"enabled": true`, `"enabled": false`, 1)) }, "switched off"},
		"gone":    {func() { os.Remove(prov) }, "gone"},
		"backend": {func() {
			writeFile(t, a.Path, strings.Replace(readFile(a.Path), `"backend": "qwenpaw"`, `"backend": "codex"`, 1))
		}, "codex"},
	} {
		t.Run(name, func(t *testing.T) {
			agent := readFile(a.Path)
			t.Cleanup(func() { writeFile(t, prov, good); writeFile(t, a.Path, agent) })
			c.mend()
			if got := a.Check(); !strings.Contains(got, c.want) {
				t.Fatalf("check %q, want it to name %q", got, c.want)
			}
		})
	}
	if got := a.Check(); got != "" {
		t.Fatalf("check after putting it back: %s", got)
	}
}

// Before QwenPaw's first start there is no agent.json to write: Connect
// says to start it, and writes nothing.
func TestQwenPawNotStartedYet(t *testing.T) {
	home, work := qwenpawHome(t)
	os.RemoveAll(work)
	os.RemoveAll(work + ".secret")
	a := qwenpawFind(t, "qwenpaw")
	if err := a.Apply("model", "uni-switch/kimi/k2"); err == nil || !strings.Contains(err.Error(), "start QwenPaw once") {
		t.Fatalf("err %v", err)
	}
	if isDir(filepath.Join(home, ".qwenpaw.secret")) {
		t.Fatal("a provider was written for a QwenPaw that never started")
	}
}

// QwenPaw's folders are where it looks for them: QWENPAW_WORKING_DIR, a
// legacy ~/.copaw, else ~/.qwenpaw; the secret folder from
// QWENPAW_SECRET_DIR, from the working folder's .env, else beside it.
func TestQwenPawFolders(t *testing.T) {
	home := t.TempDir()
	home, _ = filepath.EvalSymlinks(home)
	at := here(home)
	t.Setenv("QWENPAW_WORKING_DIR", "")
	t.Setenv("QWENPAW_SECRET_DIR", "")
	if w := qwenpawWorking(at); w != filepath.Join(home, ".qwenpaw") {
		t.Errorf("default %s", w)
	}
	os.MkdirAll(filepath.Join(home, ".copaw"), 0o755)
	if w := qwenpawWorking(at); w != filepath.Join(home, ".copaw") {
		t.Errorf("legacy %s", w)
	}
	t.Setenv("QWENPAW_WORKING_DIR", "~/paw")
	w := qwenpawWorking(at)
	if w != filepath.Join(home, "paw") {
		t.Errorf("env %s", w)
	}
	if s := qwenpawSecret(at, w); s != filepath.Join(home, "paw.secret") {
		t.Errorf("secret beside it: %s", s)
	}
	writeFile(t, filepath.Join(w, ".env"), "# QwenPaw\nexport QWENPAW_SECRET_DIR=\"~/keys\"\nOTHER=1 # x\n")
	if s := qwenpawSecret(at, w); s != filepath.Join(home, "keys") {
		t.Errorf(".env: %s", s)
	}
	t.Setenv("QWENPAW_SECRET_DIR", filepath.Join(home, "s"))
	if s := qwenpawSecret(at, w); s != filepath.Join(home, "s") {
		t.Errorf("env secret: %s", s)
	}
	for id, ok := range map[string]bool{"default": true, "a": false, "ab": true, "a-b_c": true, "-ab": false, "ab_": false, "a/b": false, "../x": false} {
		if qwenpawIDOK(id) != ok {
			t.Errorf("qwenpawIDOK(%q) = %v", id, !ok)
		}
	}
}

// A model of the user's own after uni-switch's is theirs: the row isn't on
// uni-switch, and uni-switch's provider stays in QwenPaw's list until Disconnect.
func TestQwenPawOwnModelAfterUniSwitchs(t *testing.T) {
	_, work := qwenpawHome(t)
	prov := filepath.Join(work+".secret", "providers", "custom", "uni-switch.json")
	a := qwenpawFind(t, "qwenpaw")
	if err := a.Apply("model", "uni-switch/kimi/k2"); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply("model", "deepseek/deepseek-chat"); err != nil {
		t.Fatal(err)
	}
	if a.Wired() {
		t.Fatal("wired on its own model")
	}
	if !isFile(prov) {
		t.Fatal("uni-switch's provider went with a pick of the user's own")
	}
	if got := qwenpawJSON(t, a.Path).Get("active_model").Raw; !strings.Contains(got, `"deepseek-chat"`) {
		t.Fatalf("active_model %s", got)
	}
}

// QwenPaw saves uni-switch's provider again at its start with every field of
// its own filled in (a window it detected for a model uni-switch knows none
// of, nulls, the models it discovered at the gateway): a sync after that
// writes nothing, as it does on QwenPaw 2.2.2.
func TestQwenPawSyncAfterQwenPawSaved(t *testing.T) {
	_, work := qwenpawHome(t)
	prov := filepath.Join(work+".secret", "providers", "custom", "uni-switch.json")
	a := qwenpawFind(t, "qwenpaw")
	if err := a.Apply("model", "uni-switch/kimi/k2"); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal([]byte(readFile(prov)), &m)
	m["api_key"] = "ENC:gAAAAABuni-switch"
	m["support_model_discovery"] = true
	m["model_count"] = nil
	m["discovered_models"] = []any{map[string]any{"id": "kimi/k2", "source": "discovered"}}
	for _, e := range m["extra_models"].([]any) {
		e := e.(map[string]any)
		e["availability_status"], e["config_overrides"], e["thinking_budget"] = "unverified", []any{}, nil
		if e["id"] == "kimi/k2-vision" {
			// uni-switch knows no window for it: QwenPaw's own
			e["max_input_length"], e["max_input_length_configured"], e["max_output_length"] = 131072, false, nil
		}
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	writeFile(t, prov, string(b))
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(prov); got != string(b) {
		t.Fatalf("a sync with nothing new wrote\n%s", got)
	}
}

// An agent.json whose active_model is null, as QwenPaw may write it, gets
// its null back on Disconnect.
func TestQwenPawNullModelRestored(t *testing.T) {
	_, work := qwenpawHome(t)
	path := filepath.Join(work, "workspaces", "default", "agent.json")
	body := `{
  "id": "default",
  "name": "Default Agent",
  "active_model": null,
  "language": "zh"
}
`
	writeFile(t, path, body)
	a := qwenpawFind(t, "qwenpaw")
	if err := a.Apply("model", "uni-switch/kimi/k2"); err != nil {
		t.Fatal(err)
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(path); got != body {
		t.Fatalf("Disconnect left\n%s", got)
	}
}

// QwenPaw applies an agent's thinking level only to a model whose card
// says how it thinks (Pkh on Discord: uni-switch's cards said nothing, so
// thinking_param_style was null and resolve_thinking dropped the level).
// A model of uni-switch's with reasoning levels is declared "effort" with the
// levels QwenPaw names, none and ultra left out; one without says
// nothing. A style the user set in QwenPaw stays, and one uni-switch said
// before is taken back once the model has no levels.
func TestQwenPawModelsDeclareThinking(t *testing.T) {
	_, work := qwenpawHome(t)
	prov := filepath.Join(work+".secret", "providers", "custom", "uni-switch.json")
	a := qwenpawFind(t, "qwenpaw")
	if err := a.Apply("model", "uni-switch/kimi/k2"); err != nil {
		t.Fatal(err)
	}
	p := qwenpawJSON(t, prov)
	k2 := p.Get(`extra_models.#(id=="kimi/k2")`)
	if k2.Get("thinking_param_style").String() != "effort" {
		t.Fatalf("kimi/k2 declares no effort: %s", k2.Raw)
	}
	var levels []string
	for _, l := range k2.Get("reasoning_effort_options").Array() {
		levels = append(levels, l.String())
	}
	if !slices.Equal(levels, []string{"low", "medium", "high", "max"}) {
		t.Errorf("kimi/k2's levels: %v", levels)
	}
	vision := p.Get(`extra_models.#(id=="kimi/k2-vision")`)
	if vision.Get("thinking_param_style").Exists() || vision.Get("reasoning_effort_options").Exists() {
		t.Errorf("a model with no levels declares some: %s", vision.Raw)
	}

	// the user's style for k2-vision, and uni-switch's for k2, whose levels go
	var m map[string]any
	json.Unmarshal([]byte(readFile(prov)), &m)
	for _, e := range m["extra_models"].([]any) {
		e := e.(map[string]any)
		if e["id"] == "kimi/k2-vision" {
			e["thinking_param_style"], e["reasoning_effort_options"] = "effort", []any{"high"}
			e["config_overrides"] = []any{"thinking_param_style", "reasoning_effort_options"}
		}
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	writeFile(t, prov, string(b))
	if err := catalog.SaveLive("kimi", "https://api.moonshot.cn/v1", []catalog.Model{
		{ID: "k2", Context: 262144, Output: 16384},
		{ID: "k2-vision", Images: true},
	}); err != nil {
		t.Fatal(err)
	}
	builtModels.Lock()
	builtModels.m = nil
	builtModels.Unlock()
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	p = qwenpawJSON(t, prov)
	k2 = p.Get(`extra_models.#(id=="kimi/k2")`)
	if s := k2.Get("thinking_param_style"); s.Exists() && s.Type != gjson.Null {
		t.Errorf("kimi/k2 has no levels now and still declares them: %s", k2.Raw)
	}
	vision = p.Get(`extra_models.#(id=="kimi/k2-vision")`)
	if vision.Get("thinking_param_style").String() != "effort" || vision.Get("reasoning_effort_options.0").String() != "high" {
		t.Errorf("the user's thinking settings of kimi/k2-vision lost: %s", vision.Raw)
	}
}

// The effort field is the agent's thinking_level in agent.json: a level
// is written with no thinking_budget (QwenPaw refuses one beside any
// level but budget), "" is QwenPaw's inherit, and a level QwenPaw
// doesn't name is refused. A sync leaves the level alone.
func TestQwenPawEffortField(t *testing.T) {
	_, work := qwenpawHome(t)
	path := filepath.Join(work, "workspaces", "default", "agent.json")
	var m map[string]any
	json.Unmarshal([]byte(readFile(path)), &m)
	m["thinking_level"], m["thinking_budget"] = "budget", 4096
	b, _ := json.MarshalIndent(m, "", "  ")
	writeFile(t, path, string(b))
	a := qwenpawFind(t, "qwenpaw")
	if got := a.Field("effort").Get(); got != "budget" {
		t.Fatalf("effort read %q", got)
	}
	if err := a.Apply("model", "uni-switch/kimi/k2"); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply("effort", "high"); err != nil {
		t.Fatal(err)
	}
	j := qwenpawJSON(t, path)
	if j.Get("thinking_level").String() != "high" || j.Get("thinking_budget").Exists() {
		t.Fatalf("agent.json after effort high: %s", j.Raw)
	}
	if j.Get("active_model.provider_id").String() != "uni-switch" || j.Get("fallback_models.0.model").String() != "deepseek-reasoner" {
		t.Errorf("other keys changed: %s", j.Raw)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := qwenpawJSON(t, path).Get("thinking_level").String(); got != "high" {
		t.Errorf("a sync changed the level to %q", got)
	}
	if err := a.Apply("effort", "ultra"); err == nil {
		t.Error("a level QwenPaw doesn't name was taken")
	}
	if err := a.Apply("effort", ""); err != nil {
		t.Fatal(err)
	}
	if got := qwenpawJSON(t, path).Get("thinking_level").String(); got != "inherit" {
		t.Errorf("effort \"\" wrote %q", got)
	}
	if got := a.Field("effort").Get(); got != "" {
		t.Errorf("inherit reads as %q", got)
	}
}
