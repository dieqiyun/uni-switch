package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #259: uni-switch as Codex's provider, a thread started on Codex's built-in
// provider is opened on it again (Codex keeps a thread's provider), and a
// uni-switch model picked there went to the ChatGPT backend, which refused it
// ("… is not supported when using Codex with a ChatGPT account"). The base
// URL is uni-switch's now in that state too, so the built-in provider's request
// reaches uni-switch; back on Codex's own model, it goes — but on uni-switch API,
// where Codex's own model goes through uni-switch on its ChatGPT account and
// uni-switch stays wired (#701).
func TestCodexProviderAlsoSetsBaseURL(t *testing.T) {
	for _, tc := range []struct {
		name, auth string
		api        bool
	}{
		{"signed out", "", false},
		{"api", `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, read := codexHome(t, tc.auth, "model = \"gpt-5.5\"\n")
			cx := codex(home)
			if tc.api {
				os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"), []byte(`{"models":[{"slug":"gpt-5.4","display_name":"5.4","priority":1}]}`), 0o644)
				if err := cx.Field("login").Set("api"); err != nil {
					t.Fatal(err)
				}
			}
			if err := cx.Fields[0].Set("fake/m1"); err != nil {
				t.Fatal(err)
			}
			cfg := read()
			if !strings.Contains(cfg, `model_provider = "uni-switch"`) || !strings.Contains(cfg, "model_catalog_json") ||
				!strings.Contains(cfg, `openai_base_url = "`+codexGatewayURL()+`"`) {
				t.Fatalf("\n%s", cfg)
			}
			if msg := cx.Check(); msg != "" {
				t.Fatalf("check: %s", msg)
			}
			if err := cx.Fields[0].Set("gpt-5.4"); err != nil {
				t.Fatal(err)
			}
			if tc.api {
				if cfg = read(); !strings.Contains(cfg, `openai_base_url = "`+codexGatewayURL()+`"`) ||
					!strings.Contains(cfg, `model_provider = "uni-switch"`) || !strings.Contains(cfg, `model = "codex/gpt-5.4"`) {
					t.Fatalf("own model on uni-switch API:\n%s", cfg)
				}
				return
			}
			if cfg = read(); strings.Contains(cfg, "openai_base_url") || strings.Contains(cfg, "model_provider =") ||
				!strings.Contains(cfg, `model = "gpt-5.4"`) {
				t.Fatalf("own model:\n%s", cfg)
			}
		})
	}
}

// A base URL of the user's own — a proxy of theirs in front of OpenAI — is
// left as it is when uni-switch becomes the provider, and after.
func TestCodexProviderKeepsOwnBaseURL(t *testing.T) {
	const own = `openai_base_url = "https://proxy.example/v1"`
	home, read := codexHome(t, "", "model = \"gpt-5.5\"\n"+own+"\n")
	cx := codex(home)
	if err := cx.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); !strings.Contains(cfg, own) || strings.Count(cfg, "openai_base_url") != 1 ||
		!strings.Contains(cfg, `model_provider = "uni-switch"`) {
		t.Fatalf("\n%s", cfg)
	}
	if err := cx.Fields[0].Set(""); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); !strings.Contains(cfg, own) || strings.Contains(cfg, "model_provider =") {
		t.Fatalf("reset:\n%s", cfg)
	}
}

// A Codex that an older uni-switch made its provider, with no base URL, gets
// uni-switch's at the next sync; a provider set up by hand with a catalog of
// the user's own keeps its keys, and only the table it names comes back
// (Codex loads no config while it is missing).
func TestCodexSyncAddsBaseURLToProvider(t *testing.T) {
	home, read := codexHome(t, "", "")
	cx := codex(home)
	if err := cx.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	// as an older uni-switch left it
	cfg := read()
	i := strings.Index(cfg, "openai_base_url")
	if i < 0 {
		t.Fatalf("\n%s", cfg)
	}
	j := strings.Index(cfg[i:], "\n")
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte(cfg[:i]+cfg[i+j+1:]), 0o644); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(), "openai_base_url") {
		t.Fatal("base URL still there")
	}
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if cfg = read(); !strings.Contains(cfg, `openai_base_url = "`+codexGatewayURL()+`"`) || !strings.Contains(cfg, `model_provider = "uni-switch"`) {
		t.Fatalf("sync:\n%s", cfg)
	}

	const mine = "model = \"fake/m1\"\nmodel_provider = \"uni-switch\"\nmodel_catalog_json = \"/elsewhere/models.json\"\n"
	home, read = codexHome(t, "", mine)
	if err := codex(home).Sync(); err != nil {
		t.Fatal(err)
	}
	if cfg = read(); !strings.HasPrefix(cfg, mine) || strings.Contains(cfg, "openai_base_url") || !strings.Contains(cfg, "[model_providers.uni-switch]") {
		t.Fatalf("hand-made provider changed:\n%s", cfg)
	}
}
