package provider

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
)

// openCodeAuth is auth.json as OpenCode v1.19.0 writes it
// (JSON.stringify(data, null, 2), mode 0600), from `opencode auth login`:
// two keys of its own (Zen and Go), a vendor's key with metadata, a
// provider uni-switch has no preset for, Claude Pro/Max and Copilot sign-ins,
// and an enterprise server's well-known token.
const openCodeAuth = `{
  "opencode": {
    "type": "api",
    "key": "sk-zen-1f2e3d"
  },
  "opencode-go": {
    "type": "api",
    "key": "sk-go-9a8b7c"
  },
  "deepseek": {
    "type": "api",
    "key": "sk-ds-oc",
    "metadata": {
      "source": "login"
    }
  },
  "my-relay": {
    "type": "api",
    "key": "sk-relay-oc"
  },
  "anthropic": {
    "type": "oauth",
    "refresh": "sk-ant-ort01-abc",
    "access": "sk-ant-oat01-def",
    "expires": 1792000000000
  },
  "github-copilot": {
    "type": "oauth",
    "refresh": "gho_refresh",
    "access": "tid=abc",
    "expires": 0,
    "enterpriseUrl": "github.example.com"
  },
  "https://opencode.example.com": {
    "type": "wellknown",
    "key": "OPENCODE_TOKEN",
    "token": "wk-token"
  }
}`

func writeOpenCodeAuth(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "opencode", "auth.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(openCodeAuth), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func openCodeHome(t *testing.T) string {
	t.Helper()
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	return home
}

func openCodeSource(t *testing.T) AppSource {
	t.Helper()
	for _, s := range ImportSources() {
		if s.ID == "opencode" {
			return s
		}
	}
	t.Fatal("OpenCode isn't an import source")
	return AppSource{}
}

// OpenCode's keys come over onto uni-switch's presets: OpenCode Go's on
// OpenCode Go, Zen's on OpenCode Zen (not Go, whose host it shares), a
// vendor's on its own; its sign-ins are listed and never imported.
// auth.json is only read (@hu_xiaomi on X).
func TestImportOpenCodeAuth(t *testing.T) {
	home := openCodeHome(t)
	data := filepath.Join(home, "data")
	t.Setenv("XDG_DATA_HOME", data)
	path := writeOpenCodeAuth(t, data)
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	os.Chtimes(path, old, old)

	items := itemsOf(t, "opencode")
	if s := openCodeSource(t); s.Path != path {
		t.Fatalf("read %s, want %s", s.Path, path)
	}
	for _, ref := range []string{"opencode-go", "opencode", "deepseek", "my-relay"} {
		it := items[ref]
		if it.Skip != "it has no base URL" || it.Provider.Key != "" || it.Provider.Chat != "" || it.Provider.Anthropic != "" {
			t.Fatalf("key-only import %s invented an endpoint: %+v", ref, it)
		}
	}
	for _, ref := range []string{"anthropic", "github-copilot", "https://opencode.example.com"} {
		if it := items[ref]; it.Skip != "it is a sign-in, not a key; add the subscription in uni-switch" {
			t.Fatalf("sign-in %s: %+v", ref, it)
		}
	}
	if len(items) != 7 {
		t.Fatalf("items: %d", len(items))
	}
	// the Providers page's own look leaves OpenCode for the user to ask
	for _, s := range DiscoverSources() {
		if s.ID == "opencode" {
			t.Fatal("OpenCode read without being asked")
		}
	}

	added, err := ImportFromApps([]AppPick{{Source: "opencode", Ref: "opencode-go"}, {Source: "opencode", Ref: "opencode"}, {Source: "opencode", Ref: "anthropic"}})
	if err != nil || len(added) != 0 {
		t.Fatalf("key-only imports: %v %v", added, err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != openCodeAuth {
		t.Fatalf("auth.json changed: %v", err)
	}
	if fi, err := os.Stat(path); err != nil || !fi.ModTime().Equal(old) {
		t.Fatalf("auth.json touched: %v", err)
	}
}

// With no XDG_DATA_HOME OpenCode keeps it in ~/.local/share/opencode, on
// Windows too.
func TestImportOpenCodeAuthWithoutXDG(t *testing.T) {
	home := openCodeHome(t)
	t.Setenv("XDG_DATA_HOME", "")
	path := writeOpenCodeAuth(t, filepath.Join(home, ".local", "share"))
	if s := openCodeSource(t); s.Path != path || !s.Found || s.Error != "" {
		t.Fatalf("source: %+v", s)
	}
	if it := itemsOf(t, "opencode")["opencode-go"]; it.Skip != "it has no base URL" || it.Provider.Key != "" {
		t.Fatalf("OpenCode Go: %+v", it)
	}
}

// A file that isn't there is "not found"; one that can't be read or
// parsed is unknown, an error, never an OpenCode with nothing in it.
func TestImportOpenCodeAuthUnknown(t *testing.T) {
	home := openCodeHome(t)
	data := filepath.Join(home, "data")
	t.Setenv("XDG_DATA_HOME", data)
	if s := openCodeSource(t); s.Found || s.Error != "" || len(s.Items) != 0 {
		t.Fatalf("missing: %+v", s)
	}
	path := writeOpenCodeAuth(t, data)
	os.WriteFile(path, []byte(`{"opencode-go": {"type": "api", "key": "sk-go`), 0o600)
	if s := openCodeSource(t); !s.Found || s.Error == "" || len(s.Items) != 0 {
		t.Fatalf("cut short: %+v", s)
	}
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		return // no permission bits to take away, or none that bind root
	}
	writeOpenCodeAuth(t, data)
	os.Chmod(path, 0)
	if s := openCodeSource(t); !s.Found || s.Error == "" || len(s.Items) != 0 {
		t.Fatalf("unreadable file: %+v", s)
	}
	os.Chmod(path, 0o600)
	dir := filepath.Dir(path)
	os.Chmod(dir, 0)
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	if s := openCodeSource(t); !s.Found || s.Error == "" || len(s.Items) != 0 {
		t.Fatalf("unreadable folder: %+v", s)
	}
}

// A vendor's key whose OpenCode id is a provider uni-switch has a preset for
// comes in on that preset: the id's host in models.dev's catalog is the
// preset's own address. Each entry here is api, so a key lands on the
// preset with no skip, the way openai's or deepseek's does.
func TestImportOpenCodeVendorPresets(t *testing.T) {
	home := openCodeHome(t)
	data := filepath.Join(home, "data")
	t.Setenv("XDG_DATA_HOME", data)
	want := map[string]string{
		"zhipuai":                "zhipu",
		"zai":                    "zai",
		"alibaba":                "qwen",
		"alibaba-cn":             "qwen-cn",
		"minimax":                "minimax",
		"modelscope":             "modelscope",
		"kilo":                   "kilo",
		"cline-pass":             "clinepass",
		"kimi-code-plan-cn":      "kimi-code-cn",
		"kimi-code-plan-global":  "kimi-code",
		"stepfun-step-plan":      "stepfun-cn",
		"stepfun-ai-step-plan":   "stepfun",
		"volcengine-coding-plan": "volcengine",
		"alibaba-token-plan-cn":  "bailian-token-plan",
		"tencent-token-plan":     "tencent-cloud",
	}
	entries := ""
	for id := range want {
		entries += fmt.Sprintf(",\n  %q: {\"type\": \"api\", \"key\": \"sk-oc-%s\"}", id, id)
	}
	p := filepath.Join(data, "opencode", "auth.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{"+entries[1:]+"\n}"), 0o600); err != nil {
		t.Fatal(err)
	}
	items := itemsOf(t, "opencode")
	if len(items) != len(want) {
		t.Fatalf("items: %d, want %d", len(items), len(want))
	}
	for _, id := range slices.Sorted(maps.Keys(want)) {
		it := items[id]
		if it.Skip != "it has no base URL" || it.Provider.Key != "" || it.Provider.Preset != "" {
			t.Errorf("%s invented a preset: %+v", id, it)
		}
	}
}
