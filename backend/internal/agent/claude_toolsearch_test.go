package agent

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// TestClaudeToolSearchOn: Claude Code before 2.1.304 keeps tool search off
// behind an ANTHROPIC_BASE_URL other than Anthropic's, so every MCP tool's
// definition went on each request: uni-switch's MCP router's 245 tools were
// 124K tokens of a 341K context (cybercaius on Discord). While Claude Code
// is on uni-switch, uni-switch sets ENABLE_TOOL_SEARCH=true; it goes when Claude
// Code is taken off uni-switch, and a value of the user's, an empty one
// among them, is theirs through a switch, Sync and reset.
func TestClaudeToolSearchOn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := provider.Save(provider.Provider{ID: "v", Name: "V", Chat: "http://127.0.0.1:1/v1", Key: "k", Models: []string{"gpt-x"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".claude", "settings.json")
	writeFile(t, path, `{"theme":"dark"}`)
	a := claude(home)
	search := func() (string, bool) { return edit.GetJSON(path, "env."+claudeSearchEnv) }
	set := func(key, v string) {
		t.Helper()
		if err := a.Field(key).Set(v); err != nil {
			t.Fatal(err)
		}
	}
	sync := func() {
		t.Helper()
		if err := a.Sync(); err != nil {
			t.Fatal(err)
		}
	}

	set("model", "v/gpt-x")
	if v, _ := search(); v != "true" {
		t.Fatalf("on uni-switch: %q, want true", v)
	}
	sync()
	if v, _ := search(); v != "true" {
		t.Fatalf("Sync: %q", v)
	}
	// off uni-switch, onto Claude Code's own model, and reset: uni-switch's goes
	set("model", "claude-opus-4-8")
	if v, has := search(); has {
		t.Fatalf("Claude's own model kept uni-switch's value: %q", v)
	}
	set("model", "v/gpt-x")
	if v, _ := search(); v != "true" {
		t.Fatalf("back on uni-switch: %q", v)
	}
	set("model", "")
	if v, has := search(); has {
		t.Fatalf("reset kept uni-switch's value: %q", v)
	}

	// the user's own value is theirs
	for _, mine := range []string{"false", "auto:5", ""} {
		if err := edit.SetJSON(path, edit.KV{Path: "env." + claudeSearchEnv, Value: mine}); err != nil {
			t.Fatal(err)
		}
		set("model", "v/gpt-x")
		sync()
		if v, has := search(); v != mine || !has {
			t.Fatalf("the user's %q became %q (%v)", mine, v, has)
		}
		set("model", "")
		if v, has := search(); v != mine || !has {
			t.Fatalf("reset took the user's %q away: %q (%v)", mine, v, has)
		}
		if err := edit.DelJSON(path, "env."+claudeSearchEnv); err != nil {
			t.Fatal(err)
		}
	}
	// uni-switch's "true" taken out by the user, then a value of theirs: uni-switch
	// doesn't write over it
	set("model", "v/gpt-x")
	if err := edit.SetJSON(path, edit.KV{Path: "env." + claudeSearchEnv, Value: "false"}); err != nil {
		t.Fatal(err)
	}
	sync()
	if v, _ := search(); v != "false" {
		t.Fatalf("the user's false after uni-switch's true became %q", v)
	}
	if v, _ := edit.GetJSON(path, "theme"); v != "dark" {
		t.Fatal("theme lost")
	}
}

// TestT3CodeToolSearchOn: T3 Code's uni-switch instance runs Claude Code on the
// gateway through its own environment, which may be a Claude Code from
// before 2.1.304: it is given ENABLE_TOOL_SEARCH=true too, and a value the
// user gave the instance stays.
func TestT3CodeToolSearchOn(t *testing.T) {
	home := syncHome(t)
	t.Setenv("T3CODE_HOME", "")
	path := filepath.Join(home, ".t3", "userdata", "settings.json")
	writeFile(t, path, `{}`)
	a := t3code(home)
	if err := a.Field("provider").Set(uniSwitchID); err != nil {
		t.Fatal(err)
	}
	search := func() []string {
		t.Helper()
		var file map[string]map[string]json.RawMessage
		if err := json.Unmarshal([]byte(readFile(path)), &file); err != nil {
			t.Fatal(err)
		}
		var vs []string
		for _, v := range t3Format(t, file["providerInstances"][uniSwitchID]).Environment {
			if v.Name == claudeSearchEnv {
				vs = append(vs, v.Value)
			}
		}
		return vs
	}
	if vs := search(); len(vs) != 1 || vs[0] != "true" {
		t.Fatalf("uni-switch's instance: %q, want [true]", vs)
	}
	// the user's own value on the instance
	var file map[string]any
	if err := json.Unmarshal([]byte(readFile(path)), &file); err != nil {
		t.Fatal(err)
	}
	inst := file["providerInstances"].(map[string]any)[uniSwitchID].(map[string]any)
	env := inst["environment"].([]any)
	for i, v := range env {
		if v.(map[string]any)["name"] == claudeSearchEnv {
			env[i] = map[string]any{"name": claudeSearchEnv, "value": "false", "sensitive": false}
		}
	}
	b, _ := json.Marshal(file)
	writeFile(t, path, string(b))
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if vs := search(); len(vs) != 1 || vs[0] != "false" {
		t.Fatalf("the user's false: %q", vs)
	}
}
