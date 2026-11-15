package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// openPencilUser is settings.json as OpenPencil saves it
// (serde_json::to_string_pretty of SettingsPayload, main at 40902a3f): a
// builtin agent of the user's own, an ACP agent, a recent file and the
// chat's last pick.
const openPencilUser = `{
  "version": 1,
  "theme": "dark",
  "locale": "zh-CN",
  "mcp_port": 3100,
  "mcp_cli_enabled": [true, false, false],
  "images_advanced_open": false,
  "openverse_oauth": null,
  "openverse_credential_owner": null,
  "auto_update_enabled": true,
  "experimental_features_enabled": false,
  "connected": [false, true],
  "builtin_agents": [
    {
      "id": "builtin-1",
      "preset": "anthropic",
      "display_name": "Anthropic",
      "kind": "anthropic",
      "api_key": "sk-ant-users-own",
      "model": "claude-sonnet-5",
      "models": ["claude-sonnet-5", "claude-opus-5"],
      "base_url": "https://api.anthropic.com",
      "enabled": true
    }
  ],
  "acp_agents": [
    {
      "id": "acp-1",
      "display_name": "Gemini",
      "connection_type": "stdio",
      "command": "gemini",
      "args": ["--experimental-acp"],
      "env": {},
      "url": null,
      "enabled": true
    }
  ],
  "image_gen_profiles": [],
  "active_image_gen_profile_id": null,
  "recent_files": [{"path": "/Users/me/a.op", "modified_at": 1790000000}],
  "preferred_agent_team_size": 1,
  "entry_surface": "home",
  "chat_agent": "builtin",
  "chat_model": "builtin:builtin-1:claude-sonnet-5"
}
`

// openPencilEntryDoc is a builtin_agents entry with OpenPencil's fields
// alone (BuiltinAgentPayload on main), so a field of uni-switch's own fails to
// decode.
type openPencilEntryDoc struct {
	ID          *string  `json:"id"`
	Preset      *string  `json:"preset"`
	DisplayName *string  `json:"display_name"`
	Kind        *string  `json:"kind"`
	APIKey      string   `json:"api_key"`
	Model       string   `json:"model"`
	Models      []string `json:"models"`
	BaseURL     *string  `json:"base_url"`
	Enabled     *bool    `json:"enabled"`
}

// openPencilRead is settings.json's uni-switch entry, checked as OpenPencil
// reads one: the required fields there, nothing else, a kind it knows, and
// the model list in its own form (at most 64, the first as model, models
// only for two or more); and the user's entries, as written.
func openPencilRead(t *testing.T, path string) (uniSwitch openPencilEntryDoc, others []json.RawMessage) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version *int              `json:"version"`
		Agents  []json.RawMessage `json:"builtin_agents"`
	}
	if err := json.Unmarshal(b, &doc); err != nil || doc.Version == nil || *doc.Version != 1 {
		t.Fatalf("not a settings.json OpenPencil reads (version 1 required): %v\n%s", err, b)
	}
	found := false
	for _, raw := range doc.Agents {
		var e openPencilEntryDoc
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		if err := d.Decode(&e); err != nil {
			t.Fatalf("not OpenPencil's entry: %v\n%s", err, raw)
		}
		if e.ID == nil || e.DisplayName == nil || e.Kind == nil || e.BaseURL == nil || e.Enabled == nil {
			t.Fatalf("entry without a required field: %s", raw)
		}
		if *e.ID != uniSwitchID {
			others = append(others, raw)
			continue
		}
		if found {
			t.Fatalf("two entries of uni-switch's: %s", b)
		}
		found, uniSwitch = true, e
	}
	if !found {
		t.Fatalf("no entry of uni-switch's: %s", b)
	}
	if !slices.Contains([]string{"anthropic", "openai", "openai-compat", "openai_compat"}, *uniSwitch.Kind) {
		t.Errorf("kind %q", *uniSwitch.Kind)
	}
	switch {
	case len(uniSwitch.Models) == 1, len(uniSwitch.Models) > openPencilMaxModels:
		t.Errorf("models: %d, OpenPencil writes two to 64 or none", len(uniSwitch.Models))
	case len(uniSwitch.Models) > 1 && uniSwitch.Model != uniSwitch.Models[0]:
		t.Errorf("model %q is not the first of %v", uniSwitch.Model, uniSwitch.Models)
	}
	return uniSwitch, others
}

// OpenPencil (ZSeven-W/openpencil) lists builtin agents of the user's own in
// its chat's model picker, from settings.json in the platform's config
// folder. uni-switch adds itself as the entry "uni-switch" on the gateway's Chat
// Completions, keeps the user's entries and settings as they were and the
// name and on/off they give its entry, writes nothing when nothing changed,
// and off takes only its own entry out.
func TestOpenPencilProvider(t *testing.T) {
	home := syncHome(t)
	cfg := filepath.Join(home, ".config")
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"pro", "flash"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { openPencilRunning = func() bool { return false } })
	openPencilRunning = func() bool { return false }

	a := openPencil(home, cfg)
	if !slices.ContainsFunc(All(), func(x *Agent) bool { return x.ID == "openpencil" && x.Name == "OpenPencil" }) {
		t.Fatal("OpenPencil is not among the agents")
	}
	os.MkdirAll(a.Dir, 0o755)
	if !a.Detected() {
		t.Fatal("not found with its settings folder")
	}
	path := a.Path
	os.WriteFile(path, []byte(openPencilUser), 0o600)
	f := a.Field("provider")
	if f.Get() != "" {
		t.Fatalf("get: %q", f.Get())
	}
	if err := f.Set(uniSwitchID); err != nil {
		t.Fatal(err)
	}
	if f.Get() != uniSwitchID {
		t.Fatalf("get after set: %q", f.Get())
	}

	gw := gateway.URL()
	e, others := openPencilRead(t, path)
	if *e.Kind != "openai-compat" || *e.BaseURL != gw+"/v1" || e.APIKey != agentKeyAt("openpencil", gw) || *e.Preset != "custom" || !*e.Enabled || *e.DisplayName != "uni-switch" {
		t.Errorf("entry: %+v", e)
	}
	var want []string
	for _, m := range uniSwitchModels("openpencil") {
		want = append(want, m.ID)
	}
	if len(want) < 2 || !slices.Equal(e.Models, want) {
		t.Errorf("models %v, want %v", e.Models, want)
	}
	// the user's entry, byte for byte as its JSON says, and every setting
	var user struct {
		Agents []json.RawMessage `json:"builtin_agents"`
	}
	json.Unmarshal([]byte(openPencilUser), &user)
	if len(others) != 1 || !sameJSON(string(others[0]), user.Agents[0]) {
		t.Errorf("the user's entry: %s", others)
	}
	var before, after map[string]json.RawMessage
	json.Unmarshal([]byte(openPencilUser), &before)
	b, _ := os.ReadFile(path)
	json.Unmarshal(b, &after)
	for k, v := range before {
		if k != "builtin_agents" && !sameJSON(string(after[k]), v) {
			t.Errorf("%s: %s, was %s", k, after[k], v)
		}
	}

	// nothing changed: nothing written
	st, _ := os.Stat(path)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := f.Set(uniSwitchID); err != nil {
		t.Fatal(err)
	}
	if b2, _ := os.ReadFile(path); !bytes.Equal(b, b2) {
		t.Errorf("rewritten with nothing changed:\n%s\n%s", b, b2)
	}
	if st2, _ := os.Stat(path); !st2.ModTime().Equal(st.ModTime()) {
		t.Error("written with nothing changed")
	}

	// renamed and turned off in OpenPencil's settings: a sync keeps both,
	// and puts a model gone from the list back
	var doc map[string]any
	json.Unmarshal(b, &doc)
	for _, x := range doc["builtin_agents"].([]any) {
		if m := x.(map[string]any); m["id"] == uniSwitchID {
			m["display_name"], m["enabled"] = "My uni-switch", false
			m["models"] = slices.DeleteFunc(m["models"].([]any), func(v any) bool { return v == want[1] })
		}
	}
	edited, _ := json.MarshalIndent(doc, "", "  ")
	s := string(edited)
	os.WriteFile(path, []byte(s), 0o600)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	e, _ = openPencilRead(t, path)
	if *e.DisplayName != "My uni-switch" || *e.Enabled {
		t.Errorf("the user's name and off not kept: %+v", e)
	}
	if !slices.Equal(e.Models, want) {
		t.Errorf("models after sync %v, want %v", e.Models, want)
	}

	// off: only uni-switch's entry goes, the file stays
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if f.Get() != "" {
		t.Fatalf("get after off: %q", f.Get())
	}
	b, _ = os.ReadFile(path)
	json.Unmarshal(b, &after)
	var left []json.RawMessage
	json.Unmarshal(after["builtin_agents"], &left)
	if len(left) != 1 || !sameJSON(string(left[0]), user.Agents[0]) {
		t.Errorf("after off: %s", after["builtin_agents"])
	}
	for k, v := range before {
		if !sameJSON(string(after[k]), v) {
			t.Errorf("after off, %s: %s, was %s", k, after[k], v)
		}
	}
	// a sync of an entry that isn't there writes none
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if b2, _ := os.ReadFile(path); !bytes.Equal(b, b2) {
		t.Errorf("a sync off wrote:\n%s", b2)
	}
}

// OpenPencil not run yet: uni-switch makes settings.json with the version 1
// OpenPencil needs (a file without it is set aside as corrupt), with one
// model as model alone; off takes the file away again.
func TestOpenPencilMakesItsFile(t *testing.T) {
	home := syncHome(t)
	if err := provider.Delete("relay"); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "solo", Name: "Solo", Chat: "https://api.example/v1", Key: "k", Models: []string{"one"}}); err != nil {
		t.Fatal(err)
	}
	if n := len(uniSwitchModels("openpencil")); n != 1 {
		t.Fatalf("fixture: %d models", n)
	}
	t.Cleanup(func() { openPencilRunning = func() bool { return false } })
	openPencilRunning = func() bool { return false }
	a := openPencil(home, filepath.Join(home, ".config"))
	if err := a.Field("provider").Set(uniSwitchID); err != nil {
		t.Fatal(err)
	}
	e, others := openPencilRead(t, a.Path)
	if len(others) != 0 || e.Models != nil || e.Model == "" {
		t.Errorf("one model: %+v, others %s", e, others)
	}
	b, _ := os.ReadFile(a.Path)
	var top map[string]json.RawMessage
	json.Unmarshal(b, &top)
	if len(top) != 2 || string(top["version"]) != "1" {
		t.Errorf("made: %s", b)
	}
	if err := a.Field("provider").Set(""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.Path); !os.IsNotExist(err) {
		t.Errorf("uni-switch's own file left behind: %v", err)
	}
}

// A file OpenPencil wouldn't read as its own — a version it doesn't know,
// none, a list that isn't one — is left as it is, and so is the file while
// OpenPencil runs, as it writes its settings back over uni-switch's when it
// quits.
func TestOpenPencilLeavesAlone(t *testing.T) {
	home := syncHome(t)
	running := false
	t.Cleanup(func() { openPencilRunning = func() bool { return false } })
	openPencilRunning = func() bool { return running }
	a := openPencil(home, filepath.Join(home, ".config"))
	os.MkdirAll(a.Dir, 0o755)
	for _, s := range []string{
		`{"version": 2, "builtin_agents": []}`,
		`{"theme": "dark"}`,
		`{"version": "1"}`,
		`{"version": 1, "builtin_agents": {}}`,
		`{"version": 1,`,
	} {
		os.WriteFile(a.Path, []byte(s), 0o600)
		if err := a.Field("provider").Set(uniSwitchID); err == nil {
			t.Errorf("%s: set", s)
		}
		if b, _ := os.ReadFile(a.Path); string(b) != s {
			t.Errorf("%s: written: %s", s, b)
		}
	}

	os.WriteFile(a.Path, []byte(openPencilUser), 0o600)
	running = true
	if err := a.Field("provider").Set(uniSwitchID); err != errOpenPencilRunning {
		t.Errorf("set while it runs: %v", err)
	}
	if b, _ := os.ReadFile(a.Path); string(b) != openPencilUser {
		t.Errorf("written while it runs: %s", b)
	}
	running = false
	if err := a.Field("provider").Set(uniSwitchID); err != nil {
		t.Fatal(err)
	}
	running = true
	if err := a.Field("provider").Set(""); err != errOpenPencilRunning {
		t.Errorf("off while it runs: %v", err)
	}
	if a.Field("provider").Get() != uniSwitchID {
		t.Error("taken out while it runs")
	}
}

// settings.json is where Rust's dirs::config_dir() puts OpenPencil's
// folder on each system.
func TestOpenPencilDirPerOS(t *testing.T) {
	home := filepath.Join("h", "me")
	cfg := filepath.Join(home, ".cfg")
	for _, c := range []struct{ goos, appData, want string }{
		{"darwin", "", filepath.Join(home, "Library", "Application Support", "openpencil")},
		{"linux", "", filepath.Join(cfg, "openpencil")},
		{"windows", filepath.Join("R", "Roaming"), filepath.Join("R", "Roaming", "openpencil")},
		{"windows", "", filepath.Join(home, "AppData", "Roaming", "openpencil")},
	} {
		if got := openPencilDirOn(c.goos, home, cfg, c.appData); got != c.want {
			t.Errorf("%s (APPDATA %q): %s, want %s", c.goos, c.appData, got, c.want)
		}
	}
	if got := openPencilDir(home, cfg); got != openPencilDirOn(runtime.GOOS, home, cfg, os.Getenv("APPDATA")) {
		t.Errorf("here: %s", got)
	}
}

// OpenPencil keeps 64 models of an entry and drops the rest when it next
// saves; uni-switch writes the first 64, so what it wrote and what OpenPencil
// saves back agree.
func TestOpenPencilModelCap(t *testing.T) {
	syncHome(t)
	var ms []string
	for i := range 70 {
		ms = append(ms, fmt.Sprintf("m%02d", i))
	}
	if err := provider.Save(provider.Provider{ID: "many", Name: "Many", Chat: "https://api.example/v1", Key: "k", Models: ms}); err != nil {
		t.Fatal(err)
	}
	if n := len(uniSwitchModels("openpencil")); n <= openPencilMaxModels {
		t.Fatalf("fixture: %d models", n)
	}
	e := openPencilEntry(gateway.URL())
	if len(e.Models) != openPencilMaxModels || e.Model != e.Models[0] {
		t.Errorf("%d models, model %q", len(e.Models), e.Model)
	}
}
