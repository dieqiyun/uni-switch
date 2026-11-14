package provider

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// claudeThree saves three Claude accounts as Claude Code signs in to
// each, last the Pro, and gives the helpers that sign it in and name it.
func claudeThree(t *testing.T) (prof func(email, org string), creds func(tok, plan string)) {
	t.Helper()
	home := claudeHome(t)
	cred := claudeSignIn(t, home, time.Now().Add(time.Hour))
	profile := filepath.Join(home, ".claude.json")
	prof = func(email, org string) {
		writeFile(t, profile, map[string]any{"oauthAccount": map[string]any{"emailAddress": email,
			"organizationUuid": "org-" + email, "organizationName": org}})
	}
	creds = func(tok, plan string) {
		writeFile(t, cred, map[string]any{"claudeAiOauth": map[string]any{"accessToken": "tok-" + tok, "refreshToken": "r-" + tok,
			"expiresAt": time.Now().Add(time.Hour).UnixMilli(), "subscriptionType": plan}})
	}
	for _, a := range [][3]string{{"team@x.com", "team", "Acme"}, {"max@x.com", "max", "max@x.com's Organization"}, {"pro@x.com", "pro", "pro@x.com's Organization"}} {
		prof(a[0], a[2])
		creds(a[0], a[1])
		forgetAccountCaches()
		rememberLogins(true)
	}
	return prof, creds
}

func claudeEmail(user string) string {
	email, _, _ := strings.Cut(user, " · ")
	return email
}

// netfishx on X: Pro, Max and Team accounts, Claude Code kept signed in
// to one. uni-switch switches Claude Code to the Team account, and a Claude
// Code started on the Pro a moment before writes the Pro's profile after:
// the sign-in is the Team's, the profile the Pro's. That isn't saved as
// the Pro's, whose runs (its own config directory) would then spend the
// Team and show the Team's plan and usage on the Pro's card.
func TestClaudeSwitchThenStaleProfileKeepsRecords(t *testing.T) {
	prof, _ := claudeThree(t)
	team := "team@x.com · Acme"
	if err := SwitchLogin("claude", team); err != nil {
		t.Fatal(err)
	}
	prof("pro@x.com", "pro@x.com's Organization")
	time.Sleep(10 * time.Millisecond)
	forgetAccountCaches()
	rememberLogins(true)
	if err := SwitchLogin("claude", team); err != nil { // kept: switched back
		t.Fatal(err)
	}
	forgetAccountCaches()
	rememberLogins(true)
	plans := map[string]string{"team@x.com": "team", "max@x.com": "max", "pro@x.com": "pro"}
	for _, l := range readLogins() {
		if l.Agent != "claude" {
			continue
		}
		c, _ := parseClaudeCredentials(l.Auth)
		if e := claudeEmail(l.User); c.OAuth.AccessToken != "tok-"+e || l.Plan != plans[e] {
			t.Errorf("%s holds %s, plan %q", l.User, c.OAuth.AccessToken, l.Plan)
		}
	}
	for _, l := range Logins("claude") {
		if l.Active {
			continue
		}
		dir, err := claudeSavedDir(l.User)
		if err != nil {
			t.Fatal(err)
		}
		if d, _ := readClaudeDir(dir); d.OAuth.AccessToken != "tok-"+claudeEmail(l.User) {
			t.Errorf("runs on %s use %s", l.User, d.OAuth.AccessToken)
		}
	}
}

// Records crossed before (the Pro's holding the Team's sign-in) don't stop
// the Team's own sign-in being saved: the Team's record holding it too, or
// Claude Code having renewed it since.
func TestClaudeCrossedRecordDoesntHoldTheOwnerBack(t *testing.T) {
	prof, creds := claudeThree(t)
	loginsMu.Lock()
	ls := readLogins()
	var teamAuth []byte
	for _, l := range ls {
		if strings.HasPrefix(l.User, "team@") {
			teamAuth = l.Auth
		}
	}
	for i := range ls {
		if strings.HasPrefix(ls[i].User, "pro@") {
			ls[i].Auth = teamAuth
		}
	}
	if err := writeLogins(ls); err != nil {
		t.Fatal(err)
	}
	loginsMu.Unlock()
	for _, tok := range []string{"team@x.com", "team@x.com-renewed"} {
		prof("team@x.com", "Acme")
		creds(tok, "team")
		forgetAccountCaches()
		rememberLogins(true)
		for _, l := range readLogins() {
			if c, _ := parseClaudeCredentials(l.Auth); strings.HasPrefix(l.User, "team@") && c.OAuth.AccessToken != "tok-"+tok {
				t.Errorf("Team's record holds %s, Claude Code %s", c.OAuth.AccessToken, "tok-"+tok)
			}
		}
	}
}
