package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// StringKe (#4532): moving from claude-swap to uni-switch took signing in to
// every account again, since `uni-switch accounts import claude` didn't read
// cswap's export. testdata/cswap_0.29.0_export.json is what cswap 0.29.0's
// own `cswap export` wrote (in a sandbox HOME on Linux, with made-up
// tokens): a Max account and a Team seat named "work". Each comes in
// with its sign-in as the file has it, its account and organization, and
// "work" as its name in uni-switch; Anthropic is asked nothing.
func TestImportCswapExport(t *testing.T) {
	claudeHome(t)
	asked := false
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = true
		w.WriteHeader(500)
	}))
	defer fake.Close()
	oldBase := claudeBase
	t.Cleanup(func() { claudeBase = oldBase })
	claudeBase = fake.URL

	b, err := os.ReadFile("testdata/cswap_0.29.0_export.json")
	if err != nil {
		t.Fatal(err)
	}
	rs, err := ImportLogins(context.Background(), "claude", []string{string(b)})
	if err != nil {
		t.Fatal(err)
	}
	if got := importStatuses(rs); got != "max@x.com:added,team@x.com:added" || asked {
		t.Fatalf("got %s %+v asked Anthropic: %v", got, rs, asked)
	}
	byToken := map[string]savedLogin{}
	for _, l := range readLogins() {
		byToken[loginRefreshToken(l)] = l
	}
	for _, c := range []struct{ token, email, account, org, orgName, name string }{
		{"sk-ant-ort01-ref-max", "max@x.com", "acct-max", "org-max", "max@x.com's Organization", ""},
		{"sk-ant-ort01-ref-team", "team@x.com", "acct-team", "org-acme", "Acme", "work"},
	} {
		l, ok := byToken[c.token]
		if !ok {
			t.Fatalf("%s wasn't kept: %v", c.token, byToken)
		}
		email, org := claudeWho(l.Profile)
		id, _ := claudeSessionIdentity(l.Profile)
		var p map[string]any
		_ = json.Unmarshal(l.Profile, &p)
		cr, _ := parseClaudeCredentials(l.Auth)
		if email != c.email || org != c.org || id.AccountID != c.account || p["organizationName"] != c.orgName ||
			cr.OAuth.AccessToken != strings.Replace(c.token, "ort01-ref", "oat01-acc", 1) || len(cr.OAuth.Scopes) != 3 {
			t.Fatalf("%s: %s %s %+v %v %+v", c.token, email, org, id, p, cr.OAuth)
		}
		if got := AccountNameOf("claude", l.User); got != c.name {
			t.Fatalf("%s is named %q, not %q", l.User, got, c.name)
		}
	}

	// again: both are uni-switch's already, and a name the user gave since
	// is kept
	if err := SetAccountName("claude", "team@x.com", "mine"); err != nil {
		t.Fatal(err)
	}
	rs, err = ImportLogins(context.Background(), "claude", []string{string(b)})
	if err != nil {
		t.Fatal(err)
	}
	if got := importStatuses(rs); got != "max@x.com:exists,team@x.com:exists" || AccountNameOf("claude", "team@x.com") != "mine" {
		t.Fatalf("again: %s, named %q", got, AccountNameOf("claude", "team@x.com"))
	}
}

// What cswap's own import refuses, uni-switch refuses too: an encrypted
// export, another format. A cswap API-key account is a key, a cswap file
// isn't Codex's, and a name another account has already isn't given.
func TestImportCswapRefusals(t *testing.T) {
	claudeHome(t)
	b, err := os.ReadFile("testdata/cswap_0.29.0_export.json")
	if err != nil {
		t.Fatal(err)
	}
	with := func(f func(env map[string]any)) string {
		var env map[string]any
		if err := json.Unmarshal(b, &env); err != nil {
			t.Fatal(err)
		}
		f(env)
		out, _ := json.Marshal(env)
		return string(out)
	}
	for _, c := range []struct {
		name, file, want string
	}{
		{"encrypted", with(func(env map[string]any) { env["encrypted"] = true }), "encrypted claude-swap export"},
		{"format 2", with(func(env map[string]any) { env["version"] = 2 }), "format 2"},
	} {
		if _, err := ImportLogins(context.Background(), "claude", []string{c.file}); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: %v", c.name, err)
		}
	}

	key := with(func(env map[string]any) {
		a := env["accounts"].([]any)
		k := a[0].(map[string]any)
		k["credentials"], k["kind"] = "sk-ant-api03-key", "api_key"
		env["accounts"] = a[:1]
	})
	rs, err := ImportLogins(context.Background(), "claude", []string{key})
	if err != nil || len(rs) != 1 || rs[0].Status != "failed" || !strings.Contains(rs[0].Error, "API key") {
		t.Fatalf("api key: %+v %v", rs, err)
	}
	rs, err = ImportLogins(context.Background(), "codex", []string{string(b)})
	if err != nil || len(rs) != 2 || rs[0].Status != "failed" || !strings.Contains(rs[0].Error, "not Codex's") {
		t.Fatalf("codex: %+v %v", rs, err)
	}
	if len(readLogins()) != 0 {
		t.Fatalf("saved: %+v", readLogins())
	}

	// "work" is another account's name already: the team seat comes in
	// without it
	loginsMu.Lock()
	err = writeLogins(upsertLogin(readLogins(), savedLogin{Agent: "claude", User: "pro@x.com", Auth: []byte(`{"claudeAiOauth":{"accessToken":"a","refreshToken":"r-pro"}}`)}))
	loginsMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := SetAccountName("claude", "pro@x.com", "Work"); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportLogins(context.Background(), "claude", []string{string(b)}); err != nil {
		t.Fatal(err)
	}
	if got := AccountNameOf("claude", "team@x.com"); got != "" {
		t.Fatalf("the team seat took pro's name: %q", got)
	}
	if got := AccountNameOf("claude", "pro@x.com"); got != "Work" {
		t.Fatalf("pro's name: %q", got)
	}
}

// cswapWith is the cswap export with each account's expiresAt as given
// (a missing one taken out).
func cswapWith(t *testing.T, at map[string]int64) string {
	t.Helper()
	b, err := os.ReadFile("testdata/cswap_0.29.0_export.json")
	if err != nil {
		t.Fatal(err)
	}
	var x map[string]any
	if err := json.Unmarshal(b, &x); err != nil {
		t.Fatal(err)
	}
	for _, a := range x["accounts"].([]any) {
		a := a.(map[string]any)
		o := a["credentials"].(map[string]any)["claudeAiOauth"].(map[string]any)
		if v, ok := at[a["email"].(string)]; ok && v != 0 {
			o["expiresAt"] = v
		} else if ok {
			delete(o, "expiresAt")
		}
	}
	out, _ := json.Marshal(x)
	return string(out)
}

// StringKe (#4532): importing a cswap export of the account Claude Code
// is signed in to wrote the file's sign-in over Claude Code's own. cswap
// exports what it saved when it last switched; Claude Code has refreshed
// since, and each refresh spends the refresh token before it, so the
// file's was spent and Claude Code was signed out. A sign-in uni-switch has
// that is no older than the file's (by expiresAt, which each refresh moves
// on) is kept, Claude Code's own or a saved one; a newer one in the file
// is taken, as before.
func TestImportCswapKeepsNewerSignIn(t *testing.T) {
	now := time.Now()
	setup := func(t *testing.T) (cred string) {
		home := claudeHome(t)
		cred = claudeSignIn(t, home, now.Add(time.Hour)) // refresh token sk-ant-ort01-old
		writeFile(t, filepath.Join(home, ".claude.json"), map[string]any{"oauthAccount": map[string]any{
			"emailAddress": "max@x.com", "accountUuid": "acct-max", "organizationUuid": "org-max"}})
		forgetClaudeCredential()
		rememberLogins(true)
		return cred
	}
	liveToken := func(t *testing.T, cred string) string {
		t.Helper()
		b, _ := os.ReadFile(cred)
		c, _ := parseClaudeCredentials(b)
		return c.OAuth.RefreshToken
	}
	t.Run("older than Claude Code's", func(t *testing.T) {
		cred := setup(t)
		rs, err := ImportLogins(context.Background(), "claude", []string{cswapWith(t, map[string]int64{"max@x.com": now.Add(-time.Hour).UnixMilli()})})
		if err != nil {
			t.Fatal(err)
		}
		if got := importStatuses(rs); got != "max@x.com:exists,team@x.com:added" || rs[0].Error == "" {
			t.Fatalf("got %s %+v", got, rs)
		}
		if tok := liveToken(t, cred); tok != "sk-ant-ort01-old" {
			t.Fatalf("Claude Code's sign-in written over with the file's spent one: %s", tok)
		}
	})
	t.Run("not dated", func(t *testing.T) {
		cred := setup(t)
		rs, err := ImportLogins(context.Background(), "claude", []string{cswapWith(t, map[string]int64{"max@x.com": 0})})
		if err != nil {
			t.Fatal(err)
		}
		if got := importStatuses(rs); got != "max@x.com:exists,team@x.com:added" || liveToken(t, cred) != "sk-ant-ort01-old" {
			t.Fatalf("got %s, live %s", got, liveToken(t, cred))
		}
	})
	t.Run("newer than Claude Code's", func(t *testing.T) {
		cred := setup(t)
		rs, err := ImportLogins(context.Background(), "claude", []string{cswapWith(t, map[string]int64{"max@x.com": now.Add(3 * time.Hour).UnixMilli()})})
		if err != nil {
			t.Fatal(err)
		}
		if got := importStatuses(rs); got != "max@x.com:updated,team@x.com:added" || liveToken(t, cred) != "sk-ant-ort01-ref-max" {
			t.Fatalf("got %s, live %s", got, liveToken(t, cred))
		}
	})
	t.Run("older than a saved one", func(t *testing.T) {
		setup(t)
		// team saved as Claude Code last had it, refreshed two hours on
		c := claudeCredentials{raw: map[string]any{}, OAuth: claudeAuth{AccessToken: "sk-ant-oat01-acc-team", RefreshToken: "sk-ant-ort01-ref-team",
			ExpiresAt: now.Add(2 * time.Hour).UnixMilli(), Scopes: claudeImportScopes}}
		l, err := claudeLogin(c, map[string]any{"emailAddress": "team@x.com", "accountUuid": "acct-team", "organizationUuid": "org-acme"})
		if err != nil {
			t.Fatal(err)
		}
		loginsMu.Lock()
		err = writeLogins(upsertLogin(readLogins(), l))
		loginsMu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		older := strings.ReplaceAll(cswapWith(t, map[string]int64{"team@x.com": now.Add(time.Hour).UnixMilli()}), "ref-team", "ref-team-older")
		rs, err := ImportLogins(context.Background(), "claude", []string{older})
		if err != nil {
			t.Fatal(err)
		}
		if got := importStatuses(rs); !strings.Contains(got, "team@x.com:exists") {
			t.Fatalf("got %s", got)
		}
		for _, l := range readLogins() {
			if l.User == "team@x.com" && loginRefreshToken(l) != "sk-ant-ort01-ref-team" {
				t.Fatalf("team's saved sign-in replaced by an older one: %s", loginRefreshToken(l))
			}
		}
	})
}
