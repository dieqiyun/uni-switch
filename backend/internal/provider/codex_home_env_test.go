package provider

import (
	"os"
	"path/filepath"
	"testing"
)

// With CODEX_HOME set, Codex's sign-in is read from $CODEX_HOME/auth.json,
// a switch of account writes it there, the config imported from and the
// app-server restarted are that home's, and nothing is written under
// ~/.codex (Wakkana on Discord: uni-switch read and wrote a ~/.codex Codex
// never looked at).
func TestCodexHomeEnvSignIn(t *testing.T) {
	home := signIn(t)
	ch := filepath.Join(t.TempDir(), "codex-home")
	if err := os.Rename(filepath.Join(home, ".codex"), ch); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", ch)

	if got := codexAuthPath(); got != filepath.Join(ch, "auth.json") {
		t.Fatalf("auth.json at %s", got)
	}
	if got := codexConfigPath(); got != filepath.Join(ch, "config.toml") {
		t.Fatalf("config.toml imported from %s", got)
	}
	if got := CodexHome(); got != ch {
		t.Fatalf("the app-server restarted is %s's", got)
	}
	if p, ok := codexAccount(home); !ok || p.Account.User != "me@example.com" {
		t.Fatalf("Codex's sign-in in $CODEX_HOME not read: %v %+v", ok, p.Account)
	}
	rememberLogins(true)
	codexSignIn(t, home, "work@example.com", "r-work")
	// codexSignIn writes as Codex would without CODEX_HOME; put it where
	// Codex reads it
	if err := os.Rename(filepath.Join(home, ".codex", "auth.json"), filepath.Join(ch, "auth.json")); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(home, ".codex"))
	rememberLogins(true)
	if users, active := loginUsers(Logins("codex")); len(users) != 2 || active != "work@example.com" {
		t.Fatalf("accounts %v, in use %q", users, active)
	}
	if err := SwitchLogin("codex", "me@example.com"); err != nil {
		t.Fatal(err)
	}
	if p, ok := codexAccount(home); !ok || p.Account.User != "me@example.com" {
		t.Fatalf("the switch didn't reach $CODEX_HOME/auth.json: %+v", p.Account)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("written under ~/.codex with CODEX_HOME set: %v", err)
	}
}
