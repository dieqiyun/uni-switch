package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func TestKimi(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("KIMI_SHARE_DIR", "")
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"pro", "flash"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".kimi", "config.toml")
	os.MkdirAll(filepath.Dir(path), 0o755)
	// as kimi writes it after a Kimi Code sign-in
	os.WriteFile(path, []byte(`default_model = "kimi-code/kimi-for-coding"
default_thinking = true

[models."kimi-code/kimi-for-coding"]
provider = "managed:kimi-code"
model = "kimi-for-coding"
max_context_size = 262144

[providers."managed:kimi-code"]
type = "kimi"
base_url = "https://api.kimi.com/coding/v1"
api_key = ""

[providers."managed:kimi-code".oauth]
storage = "file"
key = "oauth/kimi-code"

[loop_control]
max_steps_per_turn = 100
`), 0o644)
	read := func() string { b, _ := os.ReadFile(path); return string(b) }
	table := func(name string) map[string]string {
		t.Helper()
		got, err := edit.GetTOMLTable(path, name)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	// what kimi's own check wants: the default among the models, and each
	// model's provider among the providers
	valid := func() {
		t.Helper()
		var c struct {
			Default   string `toml:"default_model"`
			Models    map[string]struct{ Provider string }
			Providers map[string]any
		}
		if err := toml.Unmarshal([]byte(read()), &c); err != nil {
			t.Fatalf("%v\n%s", err, read())
		}
		if _, ok := c.Models[c.Default]; c.Default != "" && !ok {
			t.Fatalf("default %q is not a model:\n%s", c.Default, read())
		}
		for k, m := range c.Models {
			if c.Providers[m.Provider] == nil {
				t.Fatalf("%s: provider %q missing:\n%s", k, m.Provider, read())
			}
		}
	}
	a := kimi(home)
	if a.Path != path {
		t.Fatalf("path: %s", a.Path)
	}
	f := a.Field("model")
	if f.Get() != "kimi-code/kimi-for-coding" {
		t.Fatalf("get: %q", f.Get())
	}
	var own []string
	for _, o := range f.Options(a.Values()) {
		if !strings.HasPrefix(o.Value, "uni-switch/") {
			own = append(own, o.Value)
		}
	}
	if strings.Join(own, ",") != "kimi-code/kimi-for-coding" {
		t.Fatalf("own options: %v", own)
	}

	if err := f.Set("uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	valid()
	raw := read()
	p := table("providers.uni-switch")
	m := table(`models."uni-switch/deepseek/pro"`)
	if p["type"] != "kimi" || p["api_key"] != "uni-switch" || !strings.HasSuffix(p["base_url"], "/v1") ||
		m["provider"] != "uni-switch" || m["model"] != "deepseek/pro" || m["max_context_size"] == "" ||
		table(`models."uni-switch/deepseek/flash"`) == nil {
		t.Fatalf("tables:\n%s", raw)
	}
	if f.Get() != "uni-switch/deepseek/pro" || table(`providers."managed:kimi-code".oauth`)["key"] != "oauth/kimi-code" ||
		table("loop_control")["max_steps_per_turn"] != "100" || !strings.Contains(raw, "default_thinking = true") {
		t.Fatalf("config:\n%s", raw)
	}
	if a.Check() != "" {
		t.Fatalf("check: %s", a.Check())
	}

	// a provider added later reaches Kimi's picker
	if err := provider.Save(provider.Provider{ID: "glm", Name: "GLM", Chat: "https://open.bigmodel.cn/api/paas/v4", Key: "k", Models: []string{"glm-5"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	valid()
	if table(`models."uni-switch/glm/glm-5"`) == nil || strings.Count(read(), "[providers.uni-switch]") != 1 {
		t.Fatalf("sync:\n%s", read())
	}

	// its wiring gone while the default is still uni-switch's
	edit.SetTOMLKey(path, "providers.uni-switch", "base_url", "http://elsewhere/v1")
	if a.Check() == "" {
		t.Fatal("check missed the moved base_url")
	}

	// reset: back to the Kimi Code model the user had, uni-switch's tables gone
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	valid()
	raw = read()
	if f.Get() != "kimi-code/kimi-for-coding" || strings.Contains(raw, "uni-switch") {
		t.Fatalf("reset:\n%s", raw)
	}
	// and Sync, with uni-switch out, writes nothing back
	if err := a.Sync(); err != nil || read() != raw {
		t.Fatalf("sync after reset: %v\n%s", err, read())
	}

	// its own model from uni-switch: uni-switch's tables go too
	f.Set("uni-switch/deepseek/flash")
	if err := f.Set("kimi-code/kimi-for-coding"); err != nil {
		t.Fatal(err)
	}
	valid()
	if strings.Contains(read(), "uni-switch") {
		t.Fatalf("own:\n%s", read())
	}
}

// A kimi that has never run has no config; uni-switch writes one Kimi takes.
func TestKimiNoConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("KIMI_SHARE_DIR", filepath.Join(home, "share"))
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"pro"}}); err != nil {
		t.Fatal(err)
	}
	a := kimi(home)
	if a.Path != filepath.Join(home, "share", "config.toml") {
		t.Fatalf("path: %s", a.Path)
	}
	f := a.Field("model")
	if err := f.Set("uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(a.Path)
	var c map[string]any
	if err := toml.Unmarshal(b, &c); err != nil || c["default_model"] != "uni-switch/deepseek/pro" {
		t.Fatalf("%v\n%s", err, b)
	}
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(a.Path)
	if strings.Contains(string(b), "uni-switch") {
		t.Fatalf("reset:\n%s", b)
	}
}

// What the user set on uni-switch's provider table in Kimi Code's config.toml
// stays through every sync and pick (#2030's siblings): headers, env and a
// default model of its own. The type, address and key stay uni-switch's.
func TestKimiKeepsTheUsersSettings(t *testing.T) {
	home := syncHome(t)
	t.Setenv("KIMI_SHARE_DIR", "")
	path := filepath.Join(home, ".kimi", "config.toml")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("default_model = \"x\"\n"), 0o644)
	a := kimi(home)
	f := a.Field("model")
	if err := f.Set("uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	raw := readFile(path)
	if !strings.Contains(raw, "[providers.uni-switch]\n") {
		t.Fatalf("no table:\n%s", raw)
	}
	raw = strings.Replace(raw, "[providers.uni-switch]\n", "[providers.uni-switch]\ndefault_model = \"relay/glm-4.6\"\ncustom_headers = { X-Mine = \"1\" }\nenv = { HTTPS_PROXY = \"http://proxy:8080\" }\n", 1)
	os.WriteFile(path, []byte(raw), 0o644)
	edit.SetTOMLKey(path, "providers.uni-switch", "base_url", "http://127.0.0.1:1/v1")
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"pro"}}); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := a.Sync(); err != nil {
			t.Fatal(err)
		}
	}
	check := func(when string) {
		t.Helper()
		var c struct {
			Providers map[string]struct {
				Type    string            `toml:"type"`
				BaseURL string            `toml:"base_url"`
				APIKey  string            `toml:"api_key"`
				Default string            `toml:"default_model"`
				Headers map[string]string `toml:"custom_headers"`
				Env     map[string]string `toml:"env"`
			} `toml:"providers"`
		}
		raw := readFile(path)
		if err := toml.Unmarshal([]byte(raw), &c); err != nil {
			t.Fatalf("%v\n%s", err, raw)
		}
		p := c.Providers[uniSwitchID]
		if p.Default != "relay/glm-4.6" || p.Headers["X-Mine"] != "1" || p.Env["HTTPS_PROXY"] != "http://proxy:8080" {
			t.Errorf("%s: the user's settings not kept:\n%s", when, raw)
		}
		if p.Type != "kimi" || p.BaseURL == "http://127.0.0.1:1/v1" || p.BaseURL == "" || p.APIKey == "" {
			t.Errorf("%s: uni-switch's own parts not written:\n%s", when, raw)
		}
	}
	check("sync")
	if err := f.Set("uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	check("pick")
}
