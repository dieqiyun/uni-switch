package provider

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// A ChatGPT account OpenAI signed out (charliepu on Discord: an account
// showing no allowance until it was signed in again) reads as signed out
// on its row once its usage read says so, with a refresh tried first, and
// a sign-in anew in uni-switch gives it back. A read that fails for anything
// else marks nothing.

// revokedUsageBody is ChatGPT's 401 for a sign-in it revoked (#1793).
const revokedUsageBody = `{"error":{"message":"Encountered invalidated oauth token for user, failing request","type":null,"code":"token_revoked","param":null},"status":401}`

// deadTokens answers every token in dead with revokedUsageBody.
type deadTokens struct {
	sync.Mutex
	dead map[string]bool
	all  bool // every token, a refreshed one too
}

func (d *deadTokens) refuse(tok string) (int, string) {
	d.Lock()
	defer d.Unlock()
	if d.all || d.dead[tok] {
		return http.StatusUnauthorized, revokedUsageBody
	}
	return 0, ""
}

func codexLapsed(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, l := range Logins("codex") {
		out[l.User] = l.Lapsed
	}
	return out
}

func freshUsage() {
	loginUsageCache.Lock()
	loginUsageCache.m = nil
	loginUsageCache.Unlock()
}

// codexAccessTokens are the access tokens each account is signed in with
// now: the saved one's in logins.json, Codex's own in its auth.json.
func codexAccessTokens(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, l := range Logins("codex") {
		out[l.User] = codexAccessToken(codexLoginAuth(l))
	}
	return out
}

func codexAccessToken(b []byte) string {
	return codexGoneFor(refreshRefused("x"), "", func() []byte { return b })
}

// Revoked with a refresh that goes through: the fresh token is read, and
// nobody is signed out.
func TestCodexUsageRevokedRefreshedFirst(t *testing.T) {
	d := &deadTokens{}
	_, refreshes, _ := codexExpiredFakes(t, d.refuse, false)
	d.dead = map[string]bool{}
	for _, tok := range codexAccessTokens(t) {
		d.dead[tok] = true
	}
	u := LoginUsage(context.Background(), "codex")
	for _, user := range []string{"me@example.com", "work@example.com"} {
		if q := u[user]; q.Error != "" || len(q.Windows) != 1 {
			t.Fatalf("%s: %+v", user, q)
		}
	}
	if n := len(refreshes()); n != 2 {
		t.Fatalf("%d refreshes, want one each", n)
	}
	for user, l := range codexLapsed(t) {
		if l != "" {
			t.Fatalf("%s said signed out though its refresh went through: %q", user, l)
		}
	}
}

// Revoked and its refresh refused: both accounts' rows say to sign in
// again, Codex's own too, which used to say "run codex login" on its
// allowance line and nothing on its row. Signed in again in uni-switch, the
// account is back, its tokens replaced, not a second one added.
func TestCodexUsageRevokedSignedOutTillSignedInAgain(t *testing.T) {
	d := &deadTokens{}
	home, _, _ := codexExpiredFakes(t, d.refuse, true)
	d.dead = map[string]bool{}
	for _, tok := range codexAccessTokens(t) {
		d.dead[tok] = true
	}
	stale := codexAccessTokens(t)["work@example.com"]

	u := LoginUsage(context.Background(), "codex")
	lapsed := codexLapsed(t)
	if e := u["me@example.com"].Error; !strings.Contains(e, "add the account again") {
		t.Fatalf("saved account: %q", e)
	}
	if !strings.Contains(lapsed["me@example.com"], "add the account again") {
		t.Fatalf("saved account's row: %q", lapsed["me@example.com"])
	}
	if e := u["work@example.com"].Error; e != revokedText("work@example.com") {
		t.Fatalf("Codex's own account: %q", e)
	}
	if l := lapsed["work@example.com"]; l != revokedText("work@example.com") {
		t.Fatalf("Codex's own account's row isn't signed out: %q", l)
	}
	if _, ok := RevokedSignIn(&Account{Agent: "codex", User: "work@example.com"}, revokedReq(t, stale)); !ok {
		t.Fatal("the gateway would still send the refused sign-in")
	}

	// signed in again through uni-switch's own sign-in: the same account, its
	// tokens replaced where Codex keeps them
	exp := float64(time.Now().Add(time.Hour).Unix())
	again, err := codexLogin(fakeJWT(map[string]any{"email": "work@example.com", "https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "plus"}}),
		fakeJWT(map[string]any{"exp": exp, "n": "again"}), "r-again", "acct-work@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if using, err := addLogin(again); err != nil || !using {
		t.Fatalf("signed in again: using %v, %v", using, err)
	}
	users, active := loginUsers(Logins("codex"))
	if strings.Join(users, ",") != "me@example.com,work@example.com" || active != "work@example.com" {
		t.Fatalf("after signing in again: %v, active %q", users, active)
	}
	if got := codexAccessTokens(t)["work@example.com"]; got == stale || got == "" {
		t.Fatalf("Codex's auth.json kept the refused token")
	}
	if l := codexLapsed(t)["work@example.com"]; l != "" {
		t.Fatalf("signed in again, still said signed out: %q", l)
	}
	freshUsage()
	if q := LoginUsage(context.Background(), "codex")["work@example.com"]; q.Error != "" || len(q.Windows) != 1 {
		t.Fatalf("read after signing in again: %+v", q)
	}
	_ = home
}

// A refreshed token turned away the same: the account is signed out for
// that token, and a read that goes through later clears it.
func TestCodexUsageRevokedAfterRefreshSignedOut(t *testing.T) {
	d := &deadTokens{all: true}
	_, refreshes, _ := codexExpiredFakes(t, d.refuse, false)
	u := LoginUsage(context.Background(), "codex")
	for _, user := range []string{"me@example.com", "work@example.com"} {
		if e := u[user].Error; e != revokedText(user) {
			t.Fatalf("%s: %q", user, e)
		}
		if l := codexLapsed(t)[user]; l != revokedText(user) {
			t.Fatalf("%s's row: %q", user, l)
		}
	}
	if n := len(refreshes()); n != 2 {
		t.Fatalf("%d refreshes, want one each", n)
	}
	if _, ok := RevokedSignIn(&Account{Agent: "codex", User: "me@example.com"}, revokedReq(t, "fresh-r")); !ok {
		t.Fatal("the refreshed token turned away isn't marked")
	}

	d.Lock()
	d.all = false
	d.Unlock()
	freshUsage()
	u = LoginUsage(context.Background(), "codex")
	for _, user := range []string{"me@example.com", "work@example.com"} {
		if q := u[user]; q.Error != "" {
			t.Fatalf("%s: %+v", user, q)
		}
		if l := codexLapsed(t)[user]; l != "" {
			t.Fatalf("%s served, still said signed out: %q", user, l)
		}
	}
}

// A read that fails for anything but the sign-in is unknown: a plain 401,
// a 403, a 5xx and a network error mark no one.
func TestCodexUsageOtherFailuresMarkNobody(t *testing.T) {
	for _, c := range []struct {
		name string
		code int
		body string
	}{
		{"plain 401", 401, `{"error":{"message":"Unauthorized"}}`},
		{"403 with the words", 403, revokedUsageBody},
		{"502", 502, `bad gateway token_revoked`},
	} {
		t.Run(c.name, func(t *testing.T) {
			resetRevoked()
			t.Cleanup(resetRevoked)
			_, refreshes, _ := codexExpiredFakes(t, func(string) (int, string) { return c.code, c.body }, true)
			u := LoginUsage(context.Background(), "codex")
			for user, l := range codexLapsed(t) {
				if l != "" || strings.Contains(u[user].Error, "sign in again") {
					t.Fatalf("%s: lapsed %q, error %q", user, l, u[user].Error)
				}
			}
			if n := len(refreshes()); n != 0 {
				t.Fatalf("%d refreshes for a failure that isn't the sign-in's", n)
			}
		})
	}
	t.Run("network error", func(t *testing.T) {
		resetRevoked()
		t.Cleanup(resetRevoked)
		codexExpiredFakes(t, func(string) (int, string) { return 0, "" }, true)
		CodexBase = "http://127.0.0.1:1/backend-api/codex"
		u := LoginUsage(context.Background(), "codex")
		for user, l := range codexLapsed(t) {
			if l != "" || strings.Contains(u[user].Error, "sign in again") {
				t.Fatalf("%s: lapsed %q, error %q", user, l, u[user].Error)
			}
		}
	})
}
