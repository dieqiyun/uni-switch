package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexProviderAuthFollowsLogin(t *testing.T) {
	for _, tc := range []struct {
		name, auth, config, want string
	}{
		{"missing", "", "", "false"},
		{"API key", `{"OPENAI_API_KEY":"synthetic"}`, "", "false"},
		{"ChatGPT", `{"tokens":{"access_token":"synthetic","id_token":"x.e30.x"}}`, "", "true"},
		{"forced API", `{"tokens":{"access_token":"synthetic"}}`, "forced_login_method = \"api\"\n", "false"},
		{"unknown empty", " ", "", "true"},
		{"unknown truncated", `{"tokens":`, "", "true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, read := codexHome(t, tc.auth, tc.config)
			cx := codex(home)
			if err := cx.Field("login").Set("api"); err != nil {
				t.Fatal(err)
			}
			if err := cx.Fields[0].Set("fake/m1"); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := cx.Sync(); err != nil {
					t.Fatal(err)
				}
				if cfg := read(); !strings.Contains(cfg, "requires_openai_auth = "+tc.want) {
					t.Fatalf("want auth %s:\n%s", tc.want, cfg)
				}
				if check := cx.Check(); check != "" {
					t.Fatal(check)
				}
			}
		})
	}
}

func TestCodexProviderAuthUsesCustomHome(t *testing.T) {
	home, _ := codexHome(t, `{"tokens":{"access_token":"synthetic"}}`, "")
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	cx := codex(home)
	if err := cx.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	check := func(want string) {
		t.Helper()
		if err := cx.Sync(); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(dir, "config.toml"))
		if err != nil || !strings.Contains(string(b), "requires_openai_auth = "+want) {
			t.Fatalf("custom CODEX_HOME auth %s: %s (%v)", want, b, err)
		}
	}
	check("false")
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{"tokens":{"access_token":"synthetic"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	check("true")
	if err := os.Remove(filepath.Join(dir, "auth.json")); err != nil {
		t.Fatal(err)
	}
	check("false")
}
