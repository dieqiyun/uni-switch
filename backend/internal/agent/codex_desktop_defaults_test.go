package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
)

func assertCodexDesktopDefaults(t *testing.T, path, auth string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct{ Desktop map[string]any }
	if err := toml.Unmarshal(raw, &doc); err != nil || doc.Desktop["localeOverride"] != "zh-CN" {
		t.Fatalf("Codex UI language: %v, %v", doc.Desktop, err)
	}
	provider, err := edit.GetTOMLTable(path, "model_providers."+uniSwitchID)
	if err != nil || provider["requires_openai_auth"] != auth {
		t.Fatalf("uni-switch OpenAI auth must be %s: %v, %v", auth, provider, err)
	}
}

func TestCodexDesktopLocaleSpellings(t *testing.T) {
	for _, config := range []string{
		"desktop = { theme = \"dark\", localeOverride = \"en-US\", nested = { enabled = true } } # 用户设置\n",
		"desktop.theme = \"dark\" # 用户设置\ndesktop.localeOverride = \"en-US\"\n",
		"desktop.theme = \"dark\" # 用户设置\n",
	} {
		t.Run(config, func(t *testing.T) {
			home, read := codexHome(t, "", config)
			cx := codex(home)
			if err := cx.Fields[0].Set("fake/m1"); err != nil {
				t.Fatal(err)
			}
			assertCodexDesktopDefaults(t, cx.Path, "false")
			var doc struct{ Desktop map[string]any }
			if err := toml.Unmarshal([]byte(read()), &doc); err != nil || doc.Desktop["theme"] != "dark" || !strings.Contains(read(), "# 用户设置") {
				t.Fatalf("desktop settings lost: %v\n%s", err, read())
			}
			before := read()
			if err := cx.Sync(); err != nil || read() != before {
				t.Fatalf("stable desktop settings changed: %v\n%s", err, read())
			}
		})
	}
}

func TestCodexManagedDesktopDefaults(t *testing.T) {
	for _, c := range []struct{ name, auth, want string }{
		{"ChatGPT", `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, "true"},
		{"API key", `{"OPENAI_API_KEY":"sk-synthetic"}`, "false"},
		{"no sign-in", "", "false"},
		{"unknown sign-in", "{cut off", "true"},
	} {
		t.Run(c.name, func(t *testing.T) {
			home, read := codexHome(t, c.auth, "# 用户配置\n[desktop]\nlocaleOverride = \"en-US\" # 界面语言\ntheme = \"dark\"\n\n[model_providers.relay]\nrequires_openai_auth = false\n")
			path := filepath.Join(home, ".codex", "config.toml")
			cx := codex(home)
			if err := cx.Fields[0].Set("fake/m1"); err != nil {
				t.Fatal(err)
			}
			assertCodexDesktopDefaults(t, path, c.want)
			for _, want := range []string{"# 用户配置", "# 界面语言", `theme = "dark"`, "[model_providers.relay]\nrequires_openai_auth = false"} {
				if !strings.Contains(read(), want) {
					t.Fatalf("unrelated configuration lost: %s\n%s", want, read())
				}
			}
			if err := edit.SetTOMLKey(path, "desktop", "localeOverride", "ja-JP"); err != nil {
				t.Fatal(err)
			}
			if err := edit.SetTOMLKey(path, "model_providers."+uniSwitchID, "requires_openai_auth", false); err != nil {
				t.Fatal(err)
			}
			codexUsedUp = func() bool { return true }
			if err := cx.Sync(); err != nil {
				t.Fatal(err)
			}
			assertCodexDesktopDefaults(t, path, c.want)
			before := read()
			if err := cx.Sync(); err != nil || read() != before {
				t.Fatalf("stable defaults rewritten: %v\n%s", err, read())
			}
		})
	}
}

func TestCodexJoinedDesktopLanguageFollowsSync(t *testing.T) {
	home, _ := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, "model = \"gpt-5.5\"\n")
	path := filepath.Join(home, ".codex", "config.toml")
	cx := codex(home)
	if ok, err := cx.Join(); err != nil || !ok {
		t.Fatalf("join: %v, %v", ok, err)
	}
	assertCodexDesktopDefaults(t, path, "true")
	if err := edit.DelTOMLKey(path, "desktop", "localeOverride"); err != nil {
		t.Fatal(err)
	}
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	assertCodexDesktopDefaults(t, path, "true")
}

func TestCodexDesktopDefaultsRollback(t *testing.T) {
	home, read := codexHome(t, "", "# Invalid desktop table must not leave half a provider\n[[desktop]]\nlocaleOverride = \"en-US\"\n")
	before := read()
	if err := codex(home).Fields[0].Set("fake/m1"); err == nil {
		t.Fatal("an array desktop table accepted as settings")
	}
	if read() != before {
		t.Fatalf("failed apply changed config:\n%s", read())
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "uni-switch-models.json")); !os.IsNotExist(err) {
		t.Fatalf("failed apply left a model catalog: %v", err)
	}
}
