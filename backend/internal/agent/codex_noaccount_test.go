package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
)

// codexNoAccount says Codex's provider takes no ChatGPT account, so an
// account switch restarts nothing (danzi002 on Discord), only for a
// provider table without requires_openai_auth = true. An older false value
// counts until Sync upgrades it to the required true value.
func TestCodexNoAccountFollowsRequiresOpenAIAuth(t *testing.T) {
	home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, "")
	path := filepath.Join(home, ".codex", "config.toml")
	if codexNoAccount() {
		t.Fatalf("Codex on its own ChatGPT sign-in takes no account:\n%s", read())
	}
	cx := codex(home)
	if err := cx.Field("login").Set("api"); err != nil {
		t.Fatal(err)
	}
	if err := cx.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	if codexNoAccount() {
		t.Fatalf("uni-switch API with requires_openai_auth = true takes no account:\n%s", read())
	}
	if err := edit.SetTOMLKey(path, "model_providers."+uniSwitchID, "requires_openai_auth", false); err != nil {
		t.Fatal(err)
	}
	if !codexNoAccount() {
		t.Fatalf("false before Sync must take no account:\n%s", read())
	}
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if codexNoAccount() {
		t.Fatalf("Sync must restore the authenticated provider:\n%s", read())
	}
	// a profile may name another provider: not known, so not none
	if err := edit.SetTOMLTop(path, edit.KV{Path: "profile", Value: "work"}); err != nil {
		t.Fatal(err)
	}
	if codexNoAccount() {
		t.Fatalf("with a profile:\n%s", read())
	}
}

// The provider's table decides it, whoever's it is; the built-in openai
// provider, one with no table and a config that can't be read don't
// count.
func TestCodexNoAccountOfEachConfig(t *testing.T) {
	for _, c := range []struct {
		name, config string
		want         bool
	}{
		{"none", "", false},
		{"openai", "model_provider = \"openai\"\n", false},
		{"no table", "model_provider = \"ollama\"\n", false},
		{"uni-switch false", "model_provider = \"uni-switch\"\n[model_providers.uni-switch]\nbase_url = \"http://127.0.0.1:3425/v1\"\nrequires_openai_auth = false\n", true},
		{"uni-switch unset", "model_provider = \"uni-switch\"\n[model_providers.uni-switch]\nbase_url = \"http://127.0.0.1:3425/v1\"\n", true},
		{"uni-switch true", "model_provider = \"uni-switch\"\n[model_providers.uni-switch]\nbase_url = \"http://127.0.0.1:3425/v1\"\nrequires_openai_auth = true\n", false},
		{"theirs false", "model_provider = \"relay\"\n[model_providers.relay]\nbase_url = \"https://relay.example/v1\"\nrequires_openai_auth = false\n", true},
		{"table only", "[model_providers.uni-switch]\nrequires_openai_auth = false\n", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, c.config)
			if got := codexNoAccount(); got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
	t.Run("unreadable", func(t *testing.T) {
		home, _ := codexHome(t, "", "model_provider = \"uni-switch\"\n[model_providers.uni-switch]\nrequires_openai_auth = false\n")
		path := filepath.Join(home, ".codex", "config.toml")
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if codexNoAccount() {
			t.Fatal("a config that can't be read takes no account")
		}
	})
}
