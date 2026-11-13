package agent

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// mothxDefault is the settings.json MothX 1.3.104 writes on its first run
// (config.ensureConfigExists: two-space indent, no newline at the end),
// its skillsDir and sessionDir under the home it ran in.
func mothxDefault(home string) string {
	return strings.NewReplacer("HOME", filepath.ToSlash(home)).Replace(`{
  "defaultProvider": "deepseek-openai",
  "defaultModel": "deepseek-v4-flash",
  "defaultThinkingLevel": "medium",
  "defaultMode": "yolo",
  "tuilang": "auto",
  "toolExecution": {
    "mode": "parallel",
    "maxConcurrency": 10
  },
  "statusLine": {
    "type": "command",
    "timeoutMs": 800,
    "fallback": "builtin"
  },
  "enablePlanTool": true,
  "enableArtifact": false,
  "enableACPArtifact": false,
  "webSearch": {
    "enabled": false,
    "provider": "openai",
    "providerType": "openai-responses"
  },
  "imageGeneration": {
    "enabled": false,
    "provider": "openai",
    "apiType": "openai-images",
    "baseUrl": "https://api.openai.com/v1",
    "model": "gpt-image-1"
  },
  "contextFiles": {
    "enabled": true
  },
  "skillsDir": "HOME/.mothx/skills",
  "skillHub": {
    "defaultMarket": "skillhub.cn",
    "defaultInstallScope": "project",
    "officialHandles": [
      "user_0064faa7"
    ]
  },
  "compaction": {
    "enabled": true,
    "reserveTokens": 16384,
    "keepRecentTokens": 20000
  },
  "sandbox": {
    "enabled": false,
    "level": "none",
    "allowNetwork": false,
    "allowedRead": [
      "/usr",
      "/lib",
      "/bin",
      "/sbin",
      "/System",
      "/Library"
    ],
    "deniedPaths": [
      "/etc/shadow",
      "/etc/gshadow",
      "/etc/passwd",
      "/root"
    ],
    "passEnv": [
      "PATH",
      "HOME",
      "USER",
      "LANG",
      "LC_ALL",
      "TERM",
      "SHELL",
      "TMPDIR"
    ],
    "tmpSize": "100m",
    "protectGit": true
  },
  "sessionDir": "HOME/.mothx/sessions",
  "theme": "dark",
  "retry": {
    "enabled": true,
    "maxRetries": 5,
    "baseDelayMs": 3000
  },
  "approval": {
    "bashWhitelist": [
      "go ",
      "make ",
      "git ",
      "npm ",
      "yarn ",
      "node ",
      "python ",
      "pip "
    ],
    "confirmBeforeWrite": true
  },
  "worktree": {
    "enabled": true,
    "branchPrefix": "mothx"
  }
}`)
}

func mothxOf(t *testing.T) *Agent {
	t.Helper()
	for _, x := range All() {
		if x.ID == "mothx" {
			return x
		}
	}
	t.Fatal("no mothx in All()")
	return nil
}

// MothX (#2353) is wired in its one settings.json: uni-switch as the provider
// "uni-switch" beside the user's own, and defaultProvider/defaultModel on it.
// Everything else in the file — the settings MothX's first run spelled
// out, the user's providers, what they added to uni-switch's block — stays.
// The provider's key names MothX; Disconnect puts the file back byte for
// byte.
func TestMothX(t *testing.T) {
	home := syncHome(t)
	a := mothxOf(t)
	dir := filepath.Join(home, ".mothx")
	path := filepath.Join(dir, "settings.json")
	if runtime.GOOS == "windows" {
		dir = filepath.Join(home, "AppData", "Roaming", "mothx")
		path = filepath.Join(dir, "settings.json")
	}
	if a.Name != "MothX" || a.Icon != "mothx" || a.Bin != "mothx" || a.Dir != dir || a.Path != path || a.Detected() {
		t.Fatalf("%+v", a)
	}
	// the user's own: MothX's first-run file, and a provider of theirs
	user := mothxDefault(home)
	user = strings.Replace(user, `  "theme": "dark",`, `  "theme": "dark",
  "providers": {
    "mine": {
      "vendor": "openai",
      "baseUrl": "http://127.0.0.1:9/v1",
      "api": "openai-chat",
      "apiKey": "MINE_KEY",
      "models": [{"id": "m1", "name": "M1", "contextWindow": 64000}]
    }
  },`, 1)
	writeFile(t, path, user)
	if !a.Detected() || a.Field("model").Get() != "deepseek-openai/deepseek-v4-flash" || a.Field("effort").Get() != "medium" {
		t.Fatalf("detected %v, model %q, thinking %q", a.Detected(), a.Field("model").Get(), a.Field("effort").Get())
	}
	// its own providers' models, and the preset it is on, are offered
	var vals []string
	for _, o := range a.Field("model").Options(a.Values()) {
		vals = append(vals, o.Value)
	}
	if !slices.Contains(vals, "deepseek-openai/deepseek-v4-flash") || !slices.Contains(vals, "mine/m1") || !slices.Contains(vals, "uni-switch/relay/glm-4.6") {
		t.Fatalf("options %v", vals)
	}

	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"defaultProvider": "uni-switch", "defaultModel": "relay/glm-4.6",
		"defaultThinkingLevel": "medium", "theme": "dark", "defaultMode": "yolo", "sandbox.passEnv.2": "USER",
		"providers.mine.apiKey": "MINE_KEY", "worktree.branchPrefix": "mothx"} {
		if v, _ := edit.GetJSON(path, k); v != want {
			t.Fatalf("%s = %q:\n%s", k, v, readFile(path))
		}
	}
	var file struct {
		Providers map[string]map[string]any `json:"providers"`
	}
	if err := json.Unmarshal([]byte(readFile(path)), &file); err != nil {
		t.Fatal(err)
	}
	got := file.Providers["uni-switch"]
	if got["baseUrl"] != gateway.URL()+"/v1" || got["api"] != "openai-chat" || got["apiKey"] != "uni-switch-mothx" || got["thinkingFormat"] != "openai" {
		t.Fatalf("provider:\n%s", readFile(path))
	}
	// one entry per model of uni-switch's, in the fields MothX's ModelConfig has
	ms, _ := got["models"].([]any)
	if len(ms) == 0 {
		t.Fatalf("no models:\n%s", readFile(path))
	}
	m0 := ms[0].(map[string]any)
	if m0["id"] != "relay/glm-4.6" || m0["contextWindow"] != float64(204800) {
		t.Fatalf("model: %v", m0)
	}
	for k := range m0 {
		if !slices.Contains([]string{"id", "name", "reasoning", "input", "contextWindow", "maxTokens", "cost"}, k) {
			t.Fatalf("%s isn't a field of MothX's: %v", k, m0)
		}
	}
	if c := a.Check(); c != "" {
		t.Fatal(c)
	}
	if got := usage.AgentOf("mothx"); got != "mothx" {
		t.Fatalf("usage: %q", got)
	}

	// a key uni-switch no longer gives it is off, and a sync mends it, keeping
	// what the user put in uni-switch's block: a header, and a thinkingFormat
	// of their own
	edit.SetJSON(path, edit.KV{Path: "providers.uni-switch.apiKey", Value: gateway.Token},
		edit.KV{Path: "providers.uni-switch.headers", Value: map[string]string{"X-Mine": "1"}},
		edit.KV{Path: "providers.uni-switch.thinkingFormat", Value: "qwen"})
	if a.Check() == "" {
		t.Fatal("the shared key reads as MothX's")
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"providers.uni-switch.apiKey": "uni-switch-mothx", "providers.uni-switch.headers.X-Mine": "1",
		"providers.uni-switch.thinkingFormat": "qwen"} {
		if v, _ := edit.GetJSON(path, k); v != want {
			t.Fatalf("%s = %q after sync:\n%s", k, v, readFile(path))
		}
	}
	if c := a.Check(); c != "" {
		t.Fatal(c)
	}
	// a sync with nothing to change doesn't write
	was, _ := os.ReadFile(path)
	st, _ := os.Stat(path)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if now, _ := os.ReadFile(path); !bytes.Equal(now, was) {
		t.Fatal("a sync with nothing to change wrote")
	}
	if st2, _ := os.Stat(path); !st2.ModTime().Equal(st.ModTime()) {
		t.Fatal("a sync with nothing to change touched the file")
	}
	// the thinking level, set to what it is, isn't written either
	if err := a.Field("effort").Set("medium"); err != nil {
		t.Fatal(err)
	}
	if now, _ := os.ReadFile(path); !bytes.Equal(now, was) {
		t.Fatal("setting the level it has wrote")
	}
	// back as it was but for what the user added, and Disconnect then
	// gives back their file byte for byte
	edit.DelJSON(path, "providers.uni-switch.headers")
	edit.DelJSON(path, "providers.uni-switch.thinkingFormat")
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if a.Wired() {
		t.Fatal("still connected")
	}
	if b := readFile(path); b != user {
		t.Fatalf("settings.json not as it was:\n%s", b)
	}
	if _, err := os.Stat(path + ".before-uni-switch"); !os.IsNotExist(err) {
		t.Fatalf("the copy from before is left: %v", err)
	}
}

// With no settings.json at all (MothX installed, never run), Connect
// writes one with uni-switch alone, and Disconnect takes it away again.
func TestMothXRoundTripNoFile(t *testing.T) {
	home := syncHome(t)
	t.Setenv("MOTHX_DIR", filepath.Join(home, "mx"))
	a := mothx(home)
	os.MkdirAll(a.Dir, 0o700)
	for i := 0; i < 2; i++ {
		if err := a.Connect(); err != nil {
			t.Fatal(err)
		}
		if !a.Wired() || a.Check() != "" {
			t.Fatalf("round %d: not connected:\n%s", i, readFile(a.Path))
		}
		if err := a.Disconnect(); err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{"settings.json", "settings.json.before-uni-switch"} {
			if _, err := os.Stat(filepath.Join(a.Dir, f)); !os.IsNotExist(err) {
				t.Fatalf("round %d: %s left: %v", i, f, err)
			}
		}
	}
}

// What the user changes while it is connected stands — MothX's own /model
// dialog rewrites the whole file — and Disconnect takes uni-switch out of it
// rather than putting back the copy from before.
func TestMothXKeepsWhatChangedWhileConnected(t *testing.T) {
	home := syncHome(t)
	t.Setenv("MOTHX_DIR", filepath.Join(home, "mx"))
	a := mothx(home)
	writeFile(t, a.Path, mothxDefault(home))
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	if err := edit.SetJSON(a.Path, edit.KV{Path: "theme", Value: "light"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"theme": "light", "defaultProvider": "deepseek-openai", "defaultModel": "deepseek-v4-flash", "defaultThinkingLevel": "medium"} {
		if v, _ := edit.GetJSON(a.Path, k); v != want {
			t.Fatalf("%s = %q:\n%s", k, v, readFile(a.Path))
		}
	}
	// nor the providers object it opened in a file that had none
	if _, has := edit.GetJSON(a.Path, "providers"); has {
		t.Fatalf("uni-switch left:\n%s", readFile(a.Path))
	}
}

// $MOTHX_DIR moves its folder as MothX's own platform.ConfigDir does;
// without it, %APPDATA%\mothx on Windows and ~/.mothx elsewhere. A
// distro's MothX is at its home whatever this machine's variable says.
func TestMothXDir(t *testing.T) {
	home := syncHome(t)
	own := filepath.Join(home, "own")
	t.Setenv("MOTHX_DIR", own)
	if a := mothx(home); a.Dir != own || a.Path != filepath.Join(own, "settings.json") {
		t.Fatalf("MOTHX_DIR: %s", a.Path)
	}
	t.Setenv("MOTHX_DIR", "")
	want := filepath.Join(home, ".mothx")
	if runtime.GOOS == "windows" {
		want = filepath.Join(home, "AppData", "Roaming", "mothx")
	}
	if a := mothx(home); a.Dir != want {
		t.Fatalf("default: %s, want %s", a.Dir, want)
	}
	t.Setenv("MOTHX_DIR", own)
	root := t.TempDir()
	d := distro{Name: "Ubuntu", Root: root, Home: "/home/me", Has: map[string]bool{"dir:.mothx": true}, Mirrored: true, Running: true}
	a := wslAgent(wslKindOf("mothx"), d)
	if a.ID != "mothx@wsl:Ubuntu" || a.Name != "MothX · WSL Ubuntu" || a.Dir != filepath.Join(root, "home", "me", ".mothx") {
		t.Fatalf("%+v", a)
	}
}

// A model of the user's own is offered MothX's whole list of thinking
// levels; a model of uni-switch's says it reasons when it has efforts, which is
// what has MothX send a level at all.
func TestMothXThinkingLevels(t *testing.T) {
	home := syncHome(t)
	a := mothx(home)
	if got := a.Field("effort").Options(map[string]string{"model": "deepseek-openai/deepseek-v4-flash"}); len(got) != len(mothxLevels) {
		t.Fatalf("own model: %v", got)
	}
	if e := mothxModelJSON(catalog.Model{ID: "x", Efforts: []string{"low", "high"}}); e["reasoning"] != true {
		t.Fatalf("%v", e)
	}
	if e := mothxModelJSON(catalog.Model{ID: "y"}); e["reasoning"] != false {
		t.Fatalf("%v", e)
	}
}
