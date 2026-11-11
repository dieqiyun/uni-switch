package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The maintainer requires OpenAI auth on every uni-switch provider table,
// regardless of the sign-in or allowance, including older false values.
func TestCodexUniSwitchProviderKeepsChatGPTAccount(t *testing.T) {
	// the table as an older uni-switch wrote it, bytes from a live config.toml
	const older = "model = \"fake/m1\"\nmodel_provider = \"uni-switch\"\nmodel_catalog_json = \"uni-switch-models.json\"\n\n" +
		"[model_providers.uni-switch]\nname = \"uni-switch\"\nbase_url = \"http://127.0.0.1:3425/v1\"\nwire_api = \"responses\"\n" +
		"experimental_bearer_token = \"uni-switch\"\nhttp_headers = { \"x-openai-actor-authorization\" = \"uni-switch\" }\n"
	flag := func(t *testing.T, cfg string) string {
		t.Helper()
		i := strings.Index(cfg, "[model_providers.uni-switch]")
		if i < 0 {
			t.Fatalf("no uni-switch provider table:\n%s", cfg)
		}
		rest := cfg[i+1:]
		if j := strings.Index(rest, "\n["); j >= 0 {
			rest = rest[:j]
		}
		for _, l := range strings.Split(rest, "\n") {
			if k, v, ok := strings.Cut(l, "="); ok && strings.TrimSpace(k) == "requires_openai_auth" {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}

	t.Run("ChatGPT, uni-switch API", func(t *testing.T) {
		home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, "")
		cx := codex(home)
		if err := cx.Field("login").Set("api"); err != nil {
			t.Fatal(err)
		}
		if err := cx.Fields[0].Set("fake/m1"); err != nil {
			t.Fatal(err)
		}
		cfg := read()
		if !strings.Contains(cfg, `model_provider = "uni-switch"`) || flag(t, cfg) != "true" {
			t.Fatalf("uni-switch its provider beside a ChatGPT sign-in with room: requires_openai_auth wanted:\n%s", cfg)
		}
		if !strings.Contains(cfg, `"x-openai-actor-authorization" = "uni-switch"`) || !strings.Contains(cfg, "experimental_bearer_token") {
			t.Fatalf("uni-switch's key and actor header kept:\n%s", cfg)
		}
		// A spent account no longer removes the auth flag.
		codexUsedUp = func() bool { return true }
		if err := cx.Sync(); err != nil {
			t.Fatal(err)
		}
		if cfg := read(); flag(t, cfg) != "true" {
			t.Fatalf("used up: requires_openai_auth must stay true:\n%s", cfg)
		}
		codexUsedUp = func() bool { return false }
		if err := cx.Sync(); err != nil {
			t.Fatal(err)
		}
		if cfg := read(); flag(t, cfg) != "true" {
			t.Fatalf("room again: requires_openai_auth back:\n%s", cfg)
		}
		if c := cx.Check(); c != "" {
			t.Fatal(c)
		}
	})

	t.Run("ChatGPT, taken over", func(t *testing.T) {
		home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, "")
		cx := codex(home)
		codexUsedUp = func() bool { return true }
		if err := cx.Fields[0].Set("fake/m1"); err != nil {
			t.Fatal(err)
		}
		if cfg := read(); !strings.Contains(cfg, `model_provider = "uni-switch"`) || flag(t, cfg) != "true" {
			t.Fatalf("codex.out: uni-switch must still require OpenAI auth:\n%s", cfg)
		}
		// the reset: back beside the sign-in, the table kept for the
		// threads started on uni-switch, now keeping the account too
		codexUsedUp = func() bool { return false }
		if err := cx.Sync(); err != nil {
			t.Fatal(err)
		}
		if cfg := read(); strings.Contains(cfg, "model_provider =") || flag(t, cfg) != "true" {
			t.Fatalf("after the reset:\n%s", cfg)
		}
	})

	t.Run("older explicit false upgraded", func(t *testing.T) {
		home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, "")
		cx := codex(home)
		if err := cx.Field("login").Set("api"); err != nil {
			t.Fatal(err)
		}
		if err := cx.Fields[0].Set("fake/m1"); err != nil {
			t.Fatal(err)
		}
		cfg := read()
		cfg = strings.Replace(cfg, "requires_openai_auth = true", "requires_openai_auth = false", 1)
		if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte(cfg), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, used := range []bool{false, true, false} {
			codexUsedUp = func() bool { return used }
			if err := cx.Sync(); err != nil {
				t.Fatal(err)
			}
			if cfg := read(); flag(t, cfg) != "true" {
				t.Fatalf("used up %v: requires_openai_auth must be true:\n%s", used, cfg)
			}
		}
		codexUsedUp = func() bool { return false }
		if err := cx.Fields[0].Set("fake/m2"); err != nil {
			t.Fatal(err)
		}
		if cfg := read(); flag(t, cfg) != "true" {
			t.Fatalf("a model switch: auth flag lost:\n%s", cfg)
		}
		if c := cx.Check(); c != "" {
			t.Fatal(c)
		}
	})

	t.Run("older table synced", func(t *testing.T) {
		home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, older)
		cx := codex(home)
		if err := cx.Field("login").Set("api"); err != nil {
			t.Fatal(err)
		}
		if err := cx.Sync(); err != nil {
			t.Fatal(err)
		}
		if cfg := read(); flag(t, cfg) != "true" {
			t.Fatalf("an older uni-switch's table, synced: requires_openai_auth wanted:\n%s", cfg)
		}
	})

	for _, c := range []struct{ name, auth string }{
		{"API key", `{"OPENAI_API_KEY":"sk-relay"}`},
		{"no sign-in", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			home, read := codexHome(t, c.auth, "")
			cx := codex(home)
			if err := cx.Fields[0].Set("fake/m1"); err != nil {
				t.Fatal(err)
			}
			if cfg := read(); !strings.Contains(cfg, `model_provider = "uni-switch"`) || flag(t, cfg) != "true" {
				t.Fatalf("no ChatGPT account: requires_openai_auth still required:\n%s", cfg)
			}
		})
		// one written while signed in, the sign-in gone since
		t.Run(c.name+" after ChatGPT", func(t *testing.T) {
			withFlag := strings.Replace(older, "wire_api = \"responses\"\n", "wire_api = \"responses\"\nrequires_openai_auth = true\n", 1)
			home, read := codexHome(t, c.auth, withFlag)
			cx := codex(home)
			if err := cx.Sync(); err != nil {
				t.Fatal(err)
			}
			if cfg := read(); flag(t, cfg) != "true" {
				t.Fatalf("signed out: requires_openai_auth must stay:\n%s", cfg)
			}
		})
	}
}
