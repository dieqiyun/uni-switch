package provider

// PLUGIN-SERVED (see AGENTS.md): Kiro ("kiro") is a deprecated built-in
// subscription served by its plugin, the retired opencode-kiro-auth,
// once moved onto it (provider.Moved; the default for a new sign-in). A
// moved one's sign-ins, models, requests and usage are all the plugin's,
// never this code's (only the move, in migrate*.go, still reads its
// accounts). A fix here alone doesn't reach those users; fix the plugin
// (github.com/dieqiyun/uni-switch, packages/kiro) and raise the mover's
// min in internal/provider/migrate_kiro.go.

// Several Kiro subscriptions. kiro-cli and the Kiro IDE keep one account,
// which uni-switch only ever reads. Each account uni-switch signs in gets a home of
// its own holding its kiro-auth-token.json, refreshed there, so each refresh
// token has one holder. logins.json names those homes; Kiro's own account is
// remembered there too, with no home, so it can stand behind another or be
// turned off.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// kiroTokenFile is what an account's home holds, in the IDE's shape.
const kiroTokenFile = "kiro-auth-token.json"

// kiroAccountsDir holds the homes of the Kiro accounts uni-switch signed in.
func kiroAccountsDir() string { return filepath.Join(filepath.Dir(Path()), "kiro") }

// kiroLegacyPath is where uni-switch kept the one Kiro account it signed in
// before it kept several (v0.1.374).
func kiroLegacyPath() string { return filepath.Join(filepath.Dir(Path()), kiroTokenFile) }

// kiroLogin is a Kiro account and the home it is signed in in, "" for
// kiro-cli's or the IDE's.
type kiroLogin struct {
	Login
	Home string
}

// kiroOwnUser is who kiro-cli's or the IDE's sign-in is, "" for none; until
// Kiro has said, it is named for what it is.
func kiroOwnUser() string {
	if _, ok := readKiroAt("", ""); !ok {
		return ""
	}
	user, _ := kiroIdentity("", "")
	return firstNonEmpty(user, "Kiro account")
}

// kiroLogins lists the Kiro accounts that are signed in, the first in use
// first, then the rest as they were added.
func kiroLogins() []kiroLogin {
	kiroAdoptLegacy()
	var out []kiroLogin
	for _, l := range sideLogins("kiro", kiroOwnUser(), func(l savedLogin) bool {
		_, ok := readKiroAt("", l.Home)
		return l.Home != "" && ok
	}) {
		out = append(out, kiroLogin{l.Login, l.saved.Home})
	}
	return out
}

// kiroAdoptLegacy moves the account uni-switch kept alone into a home of its
// own, among the rest, once Kiro says whose it is; until it can, the file
// stays where it was and is tried again a few minutes on.
func kiroAdoptLegacy() {
	kiroAdopt.Lock()
	defer kiroAdopt.Unlock()
	old := kiroLegacyPath()
	if time.Since(kiroAdopt.tried) < 5*time.Minute {
		return
	}
	if _, err := os.Stat(old); err != nil {
		return
	}
	kiroAdopt.tried = time.Now()
	home, err := newKiroHome()
	if err != nil {
		return
	}
	file := filepath.Join(home, kiroTokenFile)
	if err := os.Rename(old, file); err != nil {
		removeKiroHome(home)
		return
	}
	if _, err := addKiroLogin(home); err != nil {
		_ = os.Rename(file, old)
		removeKiroHome(home)
	}
}

var kiroAdopt struct {
	sync.Mutex
	tried time.Time
}

// newKiroHome makes the home a further Kiro account is signed in in.
func newKiroHome() (string, error) {
	home := filepath.Join(kiroAccountsDir(), strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(randomToken(9))))
	return home, os.MkdirAll(home, 0o700)
}

// removeKiroHome deletes a home uni-switch made, and nothing else.
func removeKiroHome(home string) {
	if rel, err := filepath.Rel(kiroAccountsDir(), home); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		_ = os.RemoveAll(home)
	}
}

// addKiroLogin keeps the account just signed in in home, in use beside the
// others, once Kiro says whose it is. Signed in again, an account keeps
// the newer home.
func addKiroLogin(home string) (string, error) {
	user, plan := askKiroIdentity("", home)
	if user == "" {
		return "", errors.New("signed in, but Kiro didn't say whose account it is; try again")
	}
	kiroSaidNow("", home, user, plan)
	return user, addSideLogin(savedLogin{Agent: "kiro", User: user, Plan: plan, Home: home}, kiroOwnUser(),
		func(l savedLogin) { removeKiroHome(l.Home) })
}

// kiroSaved is a Kiro sign-in of uni-switch's as it is kept: the IDE's shape, with
// an AWS sign-in's client beside it.
type kiroSaved struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresAt    string `json:"expiresAt"`
	AuthMethod   string `json:"authMethod"` // social | IdC
	Provider     string `json:"provider"`   // Google, Github, BuilderId, Enterprise
	Region       string `json:"region,omitempty"`
	ProfileArn   string `json:"profileArn,omitempty"`
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
}

// readKiroFile reads a sign-in made in uni-switch.
func readKiroFile(path string) (kiroCred, bool) {
	b, err := readAccountFile(path)
	if err != nil {
		return kiroCred{}, false
	}
	var t kiroSaved
	if json.Unmarshal(b, &t) != nil || t.AccessToken == "" {
		return kiroCred{}, false
	}
	c := kiroCred{access: t.AccessToken, refresh: t.RefreshToken, region: t.Region, profile: t.ProfileArn,
		clientID: t.ClientID, clientSecret: t.ClientSecret, idePath: path, builderID: strings.EqualFold(t.Provider, "BuilderId")}
	c.expires, _ = time.Parse(time.RFC3339Nano, t.ExpiresAt)
	if c.region == "" {
		c.region = "us-east-1"
	}
	if strings.EqualFold(t.AuthMethod, "IdC") && c.clientID != "" {
		c.method = "idc"
	} else {
		c.method, c.social = "social", true
	}
	return c, true
}
