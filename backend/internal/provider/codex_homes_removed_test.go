package provider

import (
	"os"
	"path/filepath"
	"testing"
)

// charliepu on Discord (Windows, 10-31): an account Codex was signed in to
// in C:\Users\charl\.codex-db-backup-20260803, a backup no Codex runs in,
// couldn't be removed: "sign it out there (CODEX_HOME=… codex logout) to
// remove it". Now Remove takes it out of uni-switch, the folder's files left
// as they are, and it isn't read back from there at the next look. A
// sign-in made in that folder again is listed again.
func TestCodexHeldFolderAccountRemoved(t *testing.T) {
	home := signIn(t)
	t.Setenv("CODEX_HOME", "")
	backup := filepath.Join(home, ".codex-db-backup-20260803")
	auth := func(refresh string) map[string]any {
		idTok := fakeJWT(map[string]any{"email": "pro@example.com",
			"https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "pro", "chatgpt_account_id": "acct-pro"}})
		return map[string]any{"auth_mode": "chatgpt", "OPENAI_API_KEY": nil, "last_refresh": "2026-08-03T00:00:00Z",
			"tokens": map[string]any{"id_token": idTok, "access_token": fakeJWT(map[string]any{"exp": 1.0}),
				"refresh_token": refresh, "account_id": "acct-pro"}}
	}
	writeFile(t, filepath.Join(backup, "auth.json"), auth("r-old"))
	before, _ := os.ReadFile(filepath.Join(backup, "auth.json"))
	listed := func() bool {
		ForgetAccounts()
		rememberLogins(true)
		for _, l := range Logins("codex") {
			if l.User == "pro@example.com" {
				return true
			}
		}
		return false
	}
	if !listed() {
		t.Fatalf("the backup folder's account isn't listed: %+v", Logins("codex"))
	}

	if err := ForgetLogin("codex", "pro@example.com"); err != nil {
		t.Fatalf("remove the account held in the backup folder: %v", err)
	}
	if after, _ := os.ReadFile(filepath.Join(backup, "auth.json")); string(after) != string(before) {
		t.Errorf("the backup folder's auth.json was changed: %s", after)
	}
	if listed() {
		t.Fatalf("removed, then read back from the backup folder: %+v", Logins("codex"))
	}
	for _, l := range Logins("codex") {
		if l.User != "me@example.com" {
			t.Errorf("left after the removal: %+v", l)
		}
	}

	// unreadable, the record says nothing: the removed account isn't
	// listed again on a guess
	if err := os.WriteFile(codexAsidePath(), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if listed() {
		t.Fatalf("with the record unreadable, the removed account came back")
	}
	if err := setCodexAside(backup, before); err != nil {
		t.Fatal(err)
	}

	// Codex signed in in that folder again: another sign-in, listed
	writeFile(t, filepath.Join(backup, "auth.json"), auth("r-new"))
	if !listed() {
		t.Fatalf("a new sign-in in the folder isn't listed: %+v", Logins("codex"))
	}
}
