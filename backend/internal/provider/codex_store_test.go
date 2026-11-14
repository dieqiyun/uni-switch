package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeKeychain is the system keychain for a test: no test reaches the
// user's own (isolate puts an empty one in).
type fakeKeychain struct {
	mu           sync.Mutex
	items        map[string][]byte // by account
	findErr      error             // every Find answers it
	statErr      error             // every Stat answers it
	putErr       error
	finds, puts  int
	stats        int
	removes      int
	findAccounts []string
	// gate, when set, holds every Find until it is closed: the user at
	// macOS' prompt
	gate chan struct{}
}

func (k *fakeKeychain) Find(service, account string) ([]byte, error) {
	k.mu.Lock()
	gate := k.gate
	k.finds++
	k.mu.Unlock()
	if gate != nil {
		<-gate
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.findAccounts = append(k.findAccounts, account)
	if service != codexKeychainService {
		return nil, errors.New("not Codex's service: " + service)
	}
	if k.findErr != nil {
		return nil, k.findErr
	}
	b, ok := k.items[account]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), b...), nil
}

// Stat dates the item by what it holds: it moves when the item changes,
// as macOS' modification date does.
func (k *fakeKeychain) Stat(service, account string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.stats++
	if k.statErr != nil {
		return "", k.statErr
	}
	b, ok := k.items[account]
	if !ok || service != codexKeychainService {
		return "", fs.ErrNotExist
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8]), nil
}

func (k *fakeKeychain) set(account string, b []byte) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.items[account] = b
}

func (k *fakeKeychain) counts() (finds, stats int) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.finds, k.stats
}

func (k *fakeKeychain) Put(service, account string, secret []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.puts++
	if k.putErr != nil {
		return k.putErr
	}
	if k.items == nil {
		k.items = map[string][]byte{}
	}
	k.items[account] = append([]byte(nil), secret...)
	return nil
}

func (k *fakeKeychain) Remove(service, account string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.removes++
	delete(k.items, account)
	return nil
}

func (k *fakeKeychain) item(account string) []byte {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.items[account]
}

// codexInKeychain moves signIn's auth.json into a fake keychain item, as
// Codex 0.162 does with cli_auth_credentials_store = store: the item on
// one line, with a field uni-switch doesn't know, and auth.json gone.
func codexInKeychain(t *testing.T, home, store string) (*fakeKeychain, string) {
	t.Helper()
	dir := filepath.Join(home, ".codex")
	b, err := os.ReadFile(filepath.Join(dir, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(b, &m)
	m["agent_identity"] = map[string]any{"kept": "as is"}
	one, _ := json.Marshal(m)
	os.Remove(filepath.Join(dir, "auth.json"))
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte("cli_auth_credentials_store = \""+store+"\"\n"), 0o600)
	k := &fakeKeychain{items: map[string][]byte{codexKeychainAccount(dir): one}}
	codexKeychain, codexKeychainHere = k, true // isolate puts them back
	resetCodexKeychain()
	return k, codexKeychainAccount(dir)
}

// Codex's account key is its compute_store_key: SHA-256 of the canonical
// CODEX_HOME, its first 16 hex digits after "cli|". A home reached
// through a link is the same item.
func TestCodexKeychainAccountIsCodexs(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "codex-home")
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	canon, _ := filepath.EvalSymlinks(real)
	sum := sha256.Sum256([]byte(canon))
	want := "cli|" + hex.EncodeToString(sum[:])[:16]
	if got := codexKeychainAccount(real); got != want {
		t.Fatalf("account %q, want %q", got, want)
	}
	if got := codexKeychainAccount(link); got != want {
		t.Fatalf("through a link: %q, want %q", got, want)
	}
	// a home not there is hashed as given, as Codex does
	gone := filepath.Join(real, "nothing-here")
	sum = sha256.Sum256([]byte(gone))
	if got := codexKeychainAccount(gone); got != "cli|"+hex.EncodeToString(sum[:])[:16] {
		t.Fatalf("a home not there: %q", got)
	}
}

// #2367 (Dony2050): Codex set to keep its sign-in in the keychain has no
// auth.json. Its account is found there all the same: the provider, its
// saved login, the session identity and the agent's wiring all go by it.
func TestCodexSignedInInKeychain(t *testing.T) {
	for _, store := range []string{"keyring", "auto"} {
		t.Run(store, func(t *testing.T) {
			home := signIn(t)
			k, account := codexInKeychain(t, home, store)
			dir := filepath.Join(home, ".codex")
			p, ok := codexAccount(home)
			if !ok || p.Account.User != "me@example.com" {
				t.Fatalf("no Codex provider from the keychain item: %v %+v", ok, p.Account)
			}
			if l, ok := liveLogin("codex"); !ok || l.User != "me@example.com" || !strings.Contains(string(l.Auth), "agent_identity") {
				t.Fatalf("live login: %v %+v", ok, l)
			}
			if b, ok := CodexSignIn(dir); !ok || !strings.Contains(string(b), "access_token") {
				t.Fatalf("CodexSignIn: %v %s", ok, b)
			}
			ids := currentSessionIdentities(dir)
			if len(ids) == 0 || ids[0].Agent != "codex" || ids[0].User != "me@example.com" {
				t.Fatalf("session identities %+v", ids)
			}
			if k.findAccounts[0] != account {
				t.Fatalf("asked the keychain for %q, want %q", k.findAccounts[0], account)
			}
			if _, err := os.Stat(filepath.Join(dir, "auth.json")); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("auth.json written by reading: %v", err)
			}
		})
	}
}

// auto with no item is auth.json, as Codex falls back to it; keyring with
// no item is signed out, whatever auth.json is left.
func TestCodexKeychainFallback(t *testing.T) {
	home := signIn(t)
	dir := filepath.Join(home, ".codex")
	codexKeychain, codexKeychainHere = &fakeKeychain{}, true
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte("cli_auth_credentials_store = \"auto\"\n"), 0o600)
	if _, ok := codexAccount(home); !ok {
		t.Fatal("auto, no item: auth.json wasn't read")
	}
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte("cli_auth_credentials_store = \"keyring\"\n"), 0o600)
	resetCodexKeychain()
	if _, ok := codexAccount(home); ok {
		t.Fatal("keyring, no item: auth.json left beside it was taken for the sign-in")
	}
	// auto, the keychain refusing: Codex reads the file then too
	codexKeychain = &fakeKeychain{findErr: errKeychainRefused, items: map[string][]byte{codexKeychainAccount(dir): []byte(`{}`)}}
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte("cli_auth_credentials_store = \"auto\"\n"), 0o600)
	resetCodexKeychain()
	if _, ok := codexAccount(home); !ok {
		t.Fatal("auto, keychain refused: auth.json wasn't read")
	}
}

// A refresh's rotated tokens go back into the item they came from, on one
// line, every other field kept; no auth.json is made.
func TestCodexKeychainRefreshWritesItem(t *testing.T) {
	home := signIn(t)
	dir := filepath.Join(home, ".codex")
	k, account := codexInKeychain(t, home, "keyring")
	var m map[string]any
	json.Unmarshal(k.item(account), &m)
	toks := m["tokens"].(map[string]any)
	toks["access_token"] = fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Minute).Unix())})
	toks["refresh_token"] = "r-work"
	b, _ := json.Marshal(m)
	k.items[account] = b
	fakeCodexRefresh(t)
	if tok, _, err := codexToken(context.Background(), dir); err != nil || tok == "" {
		t.Fatalf("codexToken: %q %v", tok, err)
	}
	got := k.item(account)
	if strings.Contains(string(got), "\n") || !strings.Contains(string(got), `"r-work-2"`) || !strings.Contains(string(got), `"agent_identity":{"kept":"as is"}`) {
		t.Fatalf("item after the refresh: %s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "auth.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the refresh wrote auth.json: %v", err)
	}
	// the next read is the rotated one, not what was kept from before
	if tok, _, err := codexToken(context.Background(), dir); err != nil || tok == "" || k.puts != 1 {
		t.Fatalf("again: %q %v, %d puts", tok, err, k.puts)
	}
}

// A switch puts the saved account into the item Codex reads, and keeps
// the one it replaces; a keychain that wouldn't say what is there has
// nothing written over it.
func TestCodexKeychainSwitch(t *testing.T) {
	home := signIn(t)
	dir := filepath.Join(home, ".codex")
	rememberLogins(true)
	codexSignIn(t, home, "work@example.com", "r-work")
	rememberLogins(true)
	k, account := codexInKeychain(t, home, "keyring")
	if from, err := switchSavedLogin("codex", "me@example.com"); err != nil || from != "work@example.com" {
		t.Fatalf("switch: %q %v", from, err)
	}
	if got := k.item(account); !strings.Contains(string(got), `"refresh_token":"r"`) {
		t.Fatalf("the item after the switch: %s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "auth.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the switch wrote auth.json beside the item: %v", err)
	}
	saved := false
	for _, l := range readLogins() {
		if l.User == "work@example.com" && strings.Contains(string(l.Auth), "agent_identity") {
			saved = true
		}
	}
	if !saved {
		t.Fatal("the account replaced wasn't saved as the keychain had it")
	}

	// Codex rotated the item since, and the keychain won't give it
	var m map[string]any
	json.Unmarshal(k.item(account), &m)
	m["last_refresh"] = "now"
	rotated, _ := json.Marshal(m)
	k.set(account, rotated)
	was := string(rotated)
	k.findErr = errKeychainRefused
	forgetCodexKeychain()
	if _, err := switchSavedLogin("codex", "work@example.com"); err == nil {
		t.Fatal("switched over a sign-in the keychain wouldn't give")
	}
	if k.puts != 1 || string(k.item(account)) != was {
		t.Fatalf("written over a sign-in not read: %d puts", k.puts)
	}
}

// Codex's encrypted auth storage (features.secret_auth_storage) and a
// system keychain uni-switch doesn't read are neither read nor written; one
// whose config.toml doesn't parse is read as auth.json, as before, and
// nothing is saved over it.
func TestCodexStoreNotReadIsLeftAlone(t *testing.T) {
	home := signIn(t)
	dir := filepath.Join(home, ".codex")
	k, _ := codexInKeychain(t, home, "keyring")
	login := []byte(`{"tokens":{"access_token":"x"}}`)
	for name, config := range map[string]string{
		"encrypted": "cli_auth_credentials_store = \"keyring\"\n[features]\nsecret_auth_storage = true\n",
		"ephemeral": "cli_auth_credentials_store = \"ephemeral\"\n",
	} {
		os.WriteFile(filepath.Join(dir, "config.toml"), []byte(config), 0o600)
		forgetCodexKeychain()
		if _, ok := codexAccount(home); ok {
			t.Errorf("%s: read as signed in", name)
		}
		if err := saveCodexLogin(dir, login); err == nil {
			t.Errorf("%s: saved", name)
		}
	}
	codexKeychainHere = false
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte("cli_auth_credentials_store = \"keyring\"\n"), 0o600)
	if _, ok := codexAccount(home); ok {
		t.Error("keychain not read here: read as signed in")
	}
	if err := saveCodexLogin(dir, login); err == nil {
		t.Error("keychain not read here: saved")
	}
	if k.finds != 0 || k.puts != 0 {
		t.Fatalf("the item was reached: %d finds, %d puts", k.finds, k.puts)
	}
	if _, err := os.Stat(filepath.Join(dir, "auth.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("auth.json written: %v", err)
	}

	codexKeychainHere = true
	os.WriteFile(filepath.Join(dir, "auth.json"), login, 0o600)
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte("cli_auth_credentials_store = [\n"), 0o600)
	if b, ok := CodexSignIn(dir); !ok || string(b) != string(login) {
		t.Fatalf("config.toml not parsed: auth.json not read: %s", b)
	}
	if err := saveCodexLogin(dir, []byte(`{"tokens":{"access_token":"y"}}`)); err == nil {
		t.Fatal("saved while where Codex keeps it isn't known")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "auth.json")); string(b) != string(login) {
		t.Fatalf("auth.json written over: %s", b)
	}
}

// Reading the secret may put a prompt in front of the user: reads close
// together ask once, a read kept is checked by the item's date (no
// prompt) and not asked again while that is unchanged, and a refusal
// isn't asked again for minutes, a look at the accounts afresh
// (forgetCodexKeychain) included (#2367: with a one-time Allow, Dony2050
// was asked again every half minute).
func TestCodexKeychainAskedSeldom(t *testing.T) {
	home := signIn(t)
	k, account := codexInKeychain(t, home, "keyring")
	for range 20 {
		if _, ok := codexAccount(home); !ok {
			t.Fatal("not read")
		}
	}
	if finds, _ := k.counts(); finds != 1 {
		t.Fatalf("%d keychain asks for 20 reads", finds)
	}
	// later, and after the accounts are looked at afresh: the item is
	// looked at, not asked for, while it is unchanged
	for range 3 {
		ageCodexKeychain(account, time.Hour)
		forgetCodexKeychain()
		if _, ok := codexAccount(home); !ok {
			t.Fatal("not read later")
		}
		if b, err := findCodexKeychain(filepath.Join(home, ".codex"), true); err != nil || len(b) == 0 {
			t.Fatalf("a read to be spent: %v", err)
		}
	}
	if finds, stats := k.counts(); finds != 1 || stats < 4 {
		t.Fatalf("an unchanged item asked again: %d asks, %d looks", finds, stats)
	}
	// Codex changed it: asked again, once
	var m map[string]any
	json.Unmarshal(k.item(account), &m)
	m["last_refresh"] = "now"
	b, _ := json.Marshal(m)
	k.set(account, b)
	ageCodexKeychain(account, time.Hour)
	if got, err := findCodexKeychain(filepath.Join(home, ".codex"), false); err != nil || !strings.Contains(string(got), "last_refresh") {
		t.Fatalf("the changed item: %s %v", got, err)
	}
	if finds, _ := k.counts(); finds != 2 {
		t.Fatalf("a changed item: %d asks", finds)
	}
	// gone: signed out, found without asking
	k.mu.Lock()
	delete(k.items, account)
	k.mu.Unlock()
	ageCodexKeychain(account, time.Hour)
	if _, err := findCodexKeychain(filepath.Join(home, ".codex"), false); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("an item gone: %v", err)
	}
	if finds, _ := k.counts(); finds != 2 {
		t.Fatalf("an item gone was asked for: %d asks", finds)
	}

	k.set(account, b)
	k.findErr = errKeychainRefused
	resetCodexKeychain()
	for range 20 {
		codexAccount(home)
		forgetCodexKeychain()
	}
	if finds, _ := k.counts(); finds != 3 {
		t.Fatalf("a refusal asked again: %d asks", finds-2)
	}
}

// ageCodexKeychain has what is kept for account read, and last looked
// at, ago.
func ageCodexKeychain(account string, ago time.Duration) {
	codexKeychainMu.Lock()
	defer codexKeychainMu.Unlock()
	r := codexKeychainCache[account]
	r.at, r.checked = r.at.Add(-ago), r.checked.Add(-ago)
	codexKeychainCache[account] = r
}

// The secret is read with room for the user at macOS' prompts (#2367,
// Dony2050: an Allow at 11–16s failed under one 10s bound), and nobody
// waits on the user meanwhile: no reader holds the lock its neighbours
// need, one that comes while the prompt is up is told uni-switch is waiting,
// one read is in flight however many ask, and its answer is kept.
func TestCodexKeychainPromptWaitedFor(t *testing.T) {
	home := signIn(t)
	dir := filepath.Join(home, ".codex")
	k, account := codexInKeychain(t, home, "keyring")
	gate := make(chan struct{})
	k.gate = gate
	defer func(h time.Duration) { keychainHold = h }(keychainHold)
	keychainHold = 50 * time.Millisecond

	if _, err := findCodexKeychain(dir, false); !errors.Is(err, errKeychainWaiting) {
		t.Fatalf("a read while the prompt is up: %v", err)
	}
	// others, meanwhile: told at once, nothing more put up, and the lock
	// free (an Excluded, a page, a request)
	done := make(chan error, 8)
	for range 8 {
		go func() { _, err := findCodexKeychain(dir, false); done <- err }()
	}
	for range 8 {
		select {
		case err := <-done:
			if !errors.Is(err, errKeychainWaiting) {
				t.Fatalf("a reader while the prompt is up: %v", err)
			}
		case <-time.After(30 * time.Second):
			t.Fatal("a reader waited on the prompt")
		}
	}
	if _, _, err := readCodexLogin(dir, false); !codexKeychainUnread(err) || !strings.Contains(err.Error(), "waiting for an answer") {
		t.Fatalf("readCodexLogin while the prompt is up: %v", err)
	}
	codexKeychainMu.Lock()
	ask := codexKeychainAsks[account]
	codexKeychainMu.Unlock()
	if ask == nil {
		t.Fatal("no read in flight")
	}
	// the user allows, however late
	close(gate)
	<-ask.done
	if b, err := findCodexKeychain(dir, false); err != nil || !strings.Contains(string(b), "agent_identity") {
		t.Fatalf("after the Allow: %s %v", b, err)
	}
	if finds, _ := k.counts(); finds != 1 {
		t.Fatalf("%d prompts put up for one sign-in", finds)
	}
}

// Try again (RetryCodexKeychain) drops a refusal kept and reads once; a
// refusal is its own message, apart from no answer and no item.
func TestCodexKeychainRetry(t *testing.T) {
	home := signIn(t)
	rememberLogins(true)
	k, _ := codexInKeychain(t, home, "keyring")
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	k.findErr = errKeychainRefused
	if _, ok := codexAccount(home); ok {
		t.Fatal("read though refused")
	}
	x := codexExclusion(t)
	if x == nil || !x.Retry || !strings.Contains(x.Why, "denied or cancelled") || !strings.Contains(x.Why, `"Always Allow"`) {
		t.Fatalf("refused: %+v", x)
	}
	k.findErr = nil
	if _, ok := codexAccount(home); ok {
		t.Fatal("a refusal wasn't kept")
	}
	if err := RetryCodexKeychain(); err != nil {
		t.Fatalf("Try again: %v", err)
	}
	if _, ok := codexAccount(home); !ok {
		t.Fatal("not read after Try again")
	}
	if x := codexExclusion(t); x != nil {
		t.Fatalf("signed in, yet: %+v", x)
	}
	// no answer, and the refusal again: each says so, and Try again with it
	k.findErr = errKeychainNoAnswer
	resetCodexKeychain()
	if err := RetryCodexKeychain(); !errors.Is(err, errKeychainNoAnswer) {
		t.Fatalf("Try again, no answer: %v", err)
	}
	if x := codexExclusion(t); x == nil || !x.Retry || !strings.Contains(x.Why, "no answer in time") {
		t.Fatalf("no answer: %+v", x)
	}
	// no item: signed out, nothing to try again
	k.findErr = nil
	k.mu.Lock()
	clear(k.items)
	k.mu.Unlock()
	if err := RetryCodexKeychain(); err == nil || !strings.Contains(err.Error(), "no Codex sign-in") {
		t.Fatalf("Try again, no item: %v", err)
	}
	if x := codexExclusion(t); x == nil || x.Retry {
		t.Fatalf("no item: %+v", x)
	}
}

func codexExclusion(t *testing.T) *Exclusion {
	t.Helper()
	for _, x := range Excluded() {
		if x.Agent == "codex" && x.SignedOut {
			return &x
		}
	}
	return nil
}

// Signing Codex out, as `codex logout`, takes the item and auth.json both.
func TestCodexKeychainSignOut(t *testing.T) {
	home := signIn(t)
	dir := filepath.Join(home, ".codex")
	k, account := codexInKeychain(t, home, "auto")
	os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{}`), 0o600)
	if err := deleteCodexLogin(dir); err != nil {
		t.Fatal(err)
	}
	if k.item(account) != nil {
		t.Fatal("the item was left")
	}
	if _, err := os.Stat(filepath.Join(dir, "auth.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("auth.json left: %v", err)
	}
}

// A read of Codex's sign-in that fails, or finds auth.json caught while
// Codex rewrites it in place (empty, cut off), is unknown; none there
// (codex logout, keyring with no item) and a store uni-switch doesn't read
// are known.
func TestCodexSignInKnown(t *testing.T) {
	home := signIn(t)
	dir := filepath.Join(home, ".codex")
	auth := filepath.Join(dir, "auth.json")
	whole, err := os.ReadFile(auth)
	if err != nil {
		t.Fatal(err)
	}
	check := func(when string, wantOK, wantKnown bool) {
		t.Helper()
		if _, ok, known := CodexSignInKnown(dir); ok != wantOK || known != wantKnown {
			t.Fatalf("%s: ok %v known %v, want %v %v", when, ok, known, wantOK, wantKnown)
		}
	}
	check("signed in", true, true)
	for _, mid := range []string{"", string(whole[:len(whole)/2])} {
		os.WriteFile(auth, []byte(mid), 0o600)
		check("auth.json mid-rewrite: "+mid, false, false)
	}
	os.Remove(auth)
	check("logged out", false, true)

	codexKeychain, codexKeychainHere = &fakeKeychain{}, true
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte("cli_auth_credentials_store = \"keyring\"\n"), 0o600)
	resetCodexKeychain()
	check("keyring, no item", false, true)
	codexKeychain = &fakeKeychain{findErr: errKeychainRefused, items: map[string][]byte{codexKeychainAccount(dir): []byte(`{}`)}}
	resetCodexKeychain()
	check("keyring, the keychain refusing", false, false)
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte("cli_auth_credentials_store = \"ephemeral\"\n"), 0o600)
	check("ephemeral", false, true)
}
