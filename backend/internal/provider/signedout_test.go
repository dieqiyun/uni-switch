package provider

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// Saved accounts of an agent not signed in where uni-switch looks (a uni-switch
// serve under another HOME) are said to be left out, not dropped silently.
func TestSavedButSignedOut(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if err := writeLogins([]savedLogin{
		{Agent: "codex", User: "a@x.com", Auth: []byte(`{}`)},
		{Agent: "codex", User: "b@x.com", Auth: []byte(`{}`)},
	}); err != nil {
		t.Fatal(err)
	}
	var codex *Exclusion
	for _, x := range Excluded() {
		if x.Agent == "codex" && x.SignedOut {
			codex = &x
		}
		if x.Agent == "claude" {
			t.Errorf("claude has no saved accounts, yet: %+v", x)
		}
	}
	if codex == nil || !strings.Contains(codex.Why, "2 accounts are") || !strings.Contains(codex.Why, filepath.Join(home, ".codex", "auth.json")) {
		t.Fatalf("codex: %+v", codex)
	}
	if strings.Contains(codex.Why, "keychain") {
		t.Fatalf("no keyring set, yet: %s", codex.Why)
	}
	// Codex set to keep its sign-in in the keychain (#2312, #2367). Where
	// uni-switch reads the item (macOS), an empty one is signed out, said of
	// the item; a read the keychain refused is not known, and the reason
	// says how to let uni-switch in. Where it doesn't (Linux, Windows, or
	// Codex's encrypted auth storage), it is signed in only not where
	// uni-switch looks, and the reason says how to put it there.
	os.MkdirAll(filepath.Join(home, ".codex"), 0o700)
	excluded := func() *Exclusion {
		resetCodexKeychain()
		for _, x := range Excluded() {
			if x.Agent == "codex" && x.SignedOut {
				return &x
			}
		}
		return nil
	}
	defer func(k keychain, here bool) { codexKeychain, codexKeychainHere = k, here }(codexKeychain, codexKeychainHere)
	for _, store := range []string{"keyring", "auto"} {
		config := "model = \"gpt-5.6\"\ncli_auth_credentials_store = \"" + store + "\"\n\n[profiles.x]\nmodel = \"o3\"\n"
		os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte(config), 0o600)

		codexKeychain, codexKeychainHere = &fakeKeychain{}, true
		if codex = excluded(); codex == nil || len(codex.Users) != 2 || !strings.Contains(codex.Why, "isn't signed in here") ||
			!strings.Contains(codex.Why, `keychain item "Codex Auth"`) {
			t.Fatalf("%s, no item: %+v", store, codex)
		}
		if codex.Retry {
			t.Fatalf("%s, no item: Try again offered", store)
		}
		// an item there, which the keychain won't give
		codexKeychain = &fakeKeychain{findErr: errKeychainUnread, items: map[string][]byte{codexKeychainAccount(filepath.Join(home, ".codex")): []byte(`{}`)}}
		if codex = excluded(); codex == nil || strings.Contains(codex.Why, "isn't signed in here") || !codex.Retry ||
			!strings.Contains(codex.Why, "Allow uni-switch") || !strings.Contains(codex.Why, `cli_auth_credentials_store = "file"`) {
			t.Fatalf("%s, item not read: %+v", store, codex)
		}

		codexKeychainHere = false
		if codex = excluded(); codex == nil || codex.Retry || len(codex.Users) != 2 || strings.Contains(codex.Why, "isn't signed in here") ||
			!strings.Contains(codex.Why, `cli_auth_credentials_store = "`+store+`"`) || !strings.Contains(codex.Why, `cli_auth_credentials_store = "file"`) ||
			!strings.Contains(codex.Why, "doesn't read") {
			t.Fatalf("%s, keychain not read here: %+v", store, codex)
		}
		codexKeychainHere = true
		os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte(config+"\n[features]\nsecret_auth_storage = true\n"), 0o600)
		if codex = excluded(); codex == nil || !strings.Contains(codex.Why, "encrypted auth storage") || strings.Contains(codex.Why, "isn't signed in here") {
			t.Fatalf("%s, encrypted storage: %+v", store, codex)
		}
	}
	os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("cli_auth_credentials_store = \"file\"\n"), 0o600)
	for _, x := range Excluded() {
		if x.Agent == "codex" && x.SignedOut && strings.Contains(x.Why, "keychain") {
			t.Fatalf("file store, yet: %s", x.Why)
		}
	}
	// signed in: nothing to say
	os.WriteFile(filepath.Join(home, ".codex", "auth.json"), []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"x","account_id":"acc-1"}}`), 0o600)
	for _, x := range Excluded() {
		if x.SignedOut {
			t.Errorf("signed in, yet: %+v", x)
		}
	}
}

// Claude accounts saved in uni-switch are offered while Claude Code is signed
// out — no credentials, or credentials Claude Code says are signed out —
// and are not said to be left out; one whose saved sign-in is gone is
// listed as signed out, not dropped.
func TestSavedButSignedOutClaudeServed(t *testing.T) {
	home := claudeHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if err := writeLogins([]savedLogin{
		{Agent: "claude", User: "a@x.com", Auth: []byte(`{}`)},
		{Agent: "claude", User: "b@x.com", Auth: []byte(`{"claudeAiOauth":{"accessToken":"tok-b","refreshToken":"r-b"}}`)},
	}); err != nil {
		t.Fatal(err)
	}
	check := func(state string) {
		t.Helper()
		forgetClaudeCredential()
		forgetClaudeStatus()
		for _, x := range Excluded() {
			if x.Agent == "claude" {
				t.Fatalf("%s: excluded %+v", state, x)
			}
		}
		if p, ok := claudeAccount(); !ok || p.Account.User != "b@x.com" {
			t.Fatalf("%s: account %v %+v", state, ok, p.Account)
		}
		ls := Logins("claude")
		if len(ls) != 2 || ls[0].User != "a@x.com" || ls[0].Lapsed == "" || ls[1].Lapsed != "" || !ls[1].On {
			t.Fatalf("%s: logins %+v", state, ls)
		}
	}
	check("no credentials")
	shellFakes(t)
	claudeSignIn(t, home, time.Now().Add(time.Hour))
	exe := filepath.Join(home, "claude")
	testenv.Program(t, exe, "#!/bin/sh\necho '{\"loggedIn\": false}'\n")
	claudeExecutable = func() string { return exe }
	check("auth status signed out")
}

// A saved Claude account whose Claude Code is signed out (a banned account
// logged out, say) is listed, so it can be removed from uni-switch; removing
// one drops it from uni-switch's store and leaves Claude Code's own files as
// they are.
func TestSavedButSignedOutCanBeRemoved(t *testing.T) {
	home := claudeHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if err := writeLogins([]savedLogin{
		{Agent: "claude", User: "banned@x.com", Auth: []byte(`{}`)},
		{Agent: "codex", User: "c@x.com", Auth: []byte(`{}`)},
	}); err != nil {
		t.Fatal(err)
	}
	if ls := Logins("claude"); len(ls) != 1 || ls[0].User != "banned@x.com" {
		t.Fatalf("claude: %+v", ls)
	}
	if err := ForgetLogin("claude", "banned@x.com"); err != nil {
		t.Fatal(err)
	}
	if _, ok := claudeAccount(); ok {
		t.Error("removed, yet served")
	}
	if ls := readLogins(); len(ls) != 1 || ls[0].Agent != "codex" {
		t.Errorf("logins: %+v", ls)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Errorf("Claude Code's folder was touched: %v", err)
	}
}

// The keychain item under the account is the sign-in Claude Code goes by,
// emptied or not: it reads no file beside it (upstream #1742). So an
// emptied item beside a still-parseable file is signed out, and a sign-in
// made in uni-switch is put in the item, not beside it.
func TestClaudeEmptiedKeychainItemIsTheSignInBesideAFile(t *testing.T) {
	shellFakes(t)
	home := claudeHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("USER", "tester")
	creds := claudeSignIn(t, home, time.Now().Add(time.Hour))
	bin := filepath.Join(home, "bin")
	os.MkdirAll(bin, 0o755)
	added := filepath.Join(home, "added.log")
	// what Claude Code left of a refused refresh: the item is there, and it
	// reads as JSON, only with no tokens in it
	testenv.Program(t, filepath.Join(bin, "security"), `#!/bin/sh
case "$1" in
find-generic-password)
	case "$*" in
	*"-a tester"*) echo '{"claudeAiOauth":{"accessToken":"","refreshToken":"","expiresAt":0,"refreshTokenExpiresAt":1791392301433}}' ;;
	*) exit 1 ;;
	esac ;;
add-generic-password)
	echo "$*" >> '`+added+`' ;;
*) exit 1 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	claudeKeychain = true
	forgetClaudeCredential()
	if _, loc, ok := readClaudeCredential(); ok {
		t.Fatalf("an emptied keychain item was passed over for the file beside it (%+v)", loc)
	} else if !loc.keychain || loc.account != "tester" {
		t.Fatalf("the emptied item is not where a sign-in would go: %+v", loc)
	}
	if err := putClaudeLogin(savedLogin{Agent: "claude", User: "fresh@example.com", Auth: mustJSONRaw(t, map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken": "fresh-access", "refreshToken": "fresh-refresh",
			"expiresAt": time.Now().Add(time.Hour).UnixMilli(), "subscriptionType": "max",
		}})}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(creds); strings.Contains(string(b), "fresh-access") {
		t.Fatal("the fresh sign-in went into the file beside the item Claude Code reads")
	}
	if b, _ := os.ReadFile(added); !strings.Contains(string(b), "fresh-access") {
		t.Fatalf("the fresh sign-in was not put in the keychain item: %s", b)
	}
}

// With no item under the account, the file beside it is the sign-in, as
// Claude Code reads it.
func TestClaudeFileSignInWhenNoKeychainItem(t *testing.T) {
	shellFakes(t)
	home := claudeHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("USER", "tester")
	creds := claudeSignIn(t, home, time.Now().Add(time.Hour))
	bin := filepath.Join(home, "bin")
	os.MkdirAll(bin, 0o755)
	testenv.Program(t, filepath.Join(bin, "security"), "#!/bin/sh\nexit 1\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	claudeKeychain = true
	forgetClaudeCredential()
	c, loc, ok := readClaudeCredential()
	if !ok || c.OAuth.AccessToken != "sk-ant-oat01-old" || loc.keychain || loc.path != creds {
		t.Fatalf("the file is not the sign-in where no keychain item is: %v %+v %+v", ok, c.OAuth, loc)
	}
}

// With CLAUDE_CONFIG_DIR set, Claude Code keeps its sign-in in that
// directory's own item, named for it, not in "Claude Code-credentials":
// that one is read, and a sign-in is put back there.
func TestClaudeConfigDirKeychainItem(t *testing.T) {
	shellFakes(t)
	home := claudeHome(t)
	dir := filepath.Join(home, "elsewhere")
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("USER", "tester")
	bin := filepath.Join(home, "bin")
	os.MkdirAll(bin, 0o755)
	added := filepath.Join(home, "added.log")
	testenv.Program(t, filepath.Join(bin, "security"), `#!/bin/sh
case "$1" in
find-generic-password)
	case "$*" in
	*"-s `+claudeDirService(dir)+` "*"-a tester"*) echo '{"claudeAiOauth":{"accessToken":"dir-item"}}' ;;
	*"-s Claude Code-credentials "*) echo '{"claudeAiOauth":{"accessToken":"home-item"}}' ;;
	*) exit 1 ;;
	esac ;;
add-generic-password)
	echo "$*" >> '`+added+`' ;;
*) exit 1 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	claudeKeychain = true
	forgetClaudeCredential()
	c, loc, ok := readClaudeCredential()
	if !ok || c.OAuth.AccessToken != "dir-item" || !loc.keychain {
		t.Fatalf("the config directory's item is not the sign-in: %v %+v %+v", ok, c.OAuth, loc)
	}
	if err := putClaudeLogin(savedLogin{Agent: "claude", User: "fresh@example.com", Auth: mustJSONRaw(t, map[string]any{
		"claudeAiOauth": map[string]any{"accessToken": "fresh-access", "refreshToken": "fresh-refresh"},
	})}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(added); !strings.Contains(string(b), "-s "+claudeDirService(dir)+" -a tester") || !strings.Contains(string(b), "fresh-access") {
		t.Fatalf("the sign-in was not put in the config directory's item: %s", b)
	}
}

// An item uni-switch wrote over lines comes back from `security -w` as hex,
// which Claude Code reads as no item at all (#70): beside a file that holds
// a sign-in, the file is the one it goes by, and the item is left alone.
// With no file, the item is the sign-in, written again on one line.
func TestClaudeHexKeychainItemIsNoneBesideAFile(t *testing.T) {
	shellFakes(t)
	home := claudeHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("USER", "tester")
	creds := claudeSignIn(t, home, time.Now().Add(time.Hour))
	bin := filepath.Join(home, "bin")
	os.MkdirAll(bin, 0o755)
	added := filepath.Join(home, "added.log")
	item := hex.EncodeToString([]byte("{\n  \"claudeAiOauth\": {\"accessToken\": \"hexed\", \"refreshToken\": \"r\"}\n}"))
	testenv.Program(t, filepath.Join(bin, "security"), `#!/bin/sh
case "$1" in
find-generic-password)
	case "$*" in
	*"-a tester"*) echo '`+item+`' ;;
	*) exit 1 ;;
	esac ;;
add-generic-password)
	echo "$*" >> '`+added+`' ;;
*) exit 1 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	claudeKeychain = true
	forgetClaudeCredential()
	c, loc, ok := readClaudeCredential()
	if !ok || c.OAuth.AccessToken != "sk-ant-oat01-old" || loc.keychain || loc.path != creds {
		t.Fatalf("a hex item Claude Code can't read was taken over the file beside it: %v %+v %+v", ok, c.OAuth, loc)
	}
	if b, _ := os.ReadFile(added); len(b) > 0 {
		t.Fatalf("the hex item was written while the file is the sign-in: %s", b)
	}
	os.Remove(creds)
	forgetClaudeCredential()
	c, loc, ok = readClaudeCredential()
	if !ok || c.OAuth.AccessToken != "hexed" || !loc.keychain || loc.account != "tester" {
		t.Fatalf("with no file the hex item is not the sign-in: %v %+v %+v", ok, c.OAuth, loc)
	}
	if b, _ := os.ReadFile(added); !strings.Contains(string(b), `{"claudeAiOauth":{"accessToken":"hexed"`) {
		t.Fatalf("the hex item was not written again on one line: %s", b)
	}
}

// Claude Code's sign-in is read from the keychain as Claude Code reads it,
// under its account: another item for the same service (one an earlier
// sign-in left) that comes first by service alone isn't taken for it.
func TestClaudeKeychainReadsItsAccount(t *testing.T) {
	shellFakes(t)
	home := claudeHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("USER", "tester")
	bin := filepath.Join(home, "bin")
	os.MkdirAll(bin, 0o755)
	// the leftover comes first unless the account is asked for
	testenv.Program(t, filepath.Join(bin, "security"), `#!/bin/sh
case "$*" in
*"-a tester"*) echo '{"claudeAiOauth":{"accessToken":"live","subscriptionType":"max"}}' ;;
*) echo '{}' ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	claudeKeychain = true
	forgetClaudeCredential()
	c, loc, ok := claudeCredential()
	if !ok || c.OAuth.AccessToken != "live" || !loc.keychain || loc.account != "tester" {
		t.Fatalf("read %v %+v %+v", ok, c.OAuth, loc)
	}
}
