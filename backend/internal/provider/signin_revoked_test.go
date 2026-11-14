package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Only a 401 whose words say the sign-in is gone marks it (#1793): an
// expired token is a refresh's to mend, and a 403, a 5xx or a rate limit
// says nothing about the sign-in.
func TestSignInRevokedWords(t *testing.T) {
	for _, c := range []struct {
		status int
		body   string
		want   bool
	}{
		{401, `{"error":{"message":"Encountered invalidated oauth token for user, failing request","code":"token_revoked"},"status":401}`, true},
		{401, `{"error":"invalid_grant","error_description":"Refresh token has been revoked"}`, true},
		{401, `{"error":{"code":"refresh_token_reused"}}`, true},
		{401, `{"error":{"code":"token_expired","message":"Provided authentication token is expired."}}`, false},
		{401, `{"error":{"message":"Incorrect API key provided"}}`, false},
		{403, `{"error":{"code":"token_revoked"}}`, false},
		{500, `{"error":{"code":"token_revoked"}}`, false},
		{502, `upstream connect error`, false},
		{429, `{"error":{"code":"rate_limit_exceeded"}}`, false},
	} {
		if got := SignInRevoked(c.status, []byte(c.body)); got != c.want {
			t.Errorf("%d %s: %v, want %v", c.status, c.body, got, c.want)
		}
	}
}

func revokedReq(t *testing.T, token string) *http.Request {
	t.Helper()
	req, _ := http.NewRequest("POST", "http://x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func sideLapsed(t *testing.T, agent, user string) string {
	t.Helper()
	for _, l := range sideLogins(agent, "", func(savedLogin) bool { return true }) {
		if strings.EqualFold(l.User, user) {
			return l.Lapsed
		}
	}
	t.Fatalf("no %s account %s", agent, user)
	return ""
}

// A side account (ZCode, Grok, ... signed in in uni-switch) whose sign-in its
// vendor revoked is marked for that credential, kept across a restart,
// shown on its row, and cleared by a request it serves or a sign-in anew.
func TestRevokedSideLoginMarkedTillSignedInAgain(t *testing.T) {
	signIn(t)
	resetRevoked()
	t.Cleanup(resetRevoked)
	if err := addSideLogin(savedLogin{Agent: "zcode", User: "u@example.com", Auth: json.RawMessage(`{"token":"a"}`)}, "", func(savedLogin) {}); err != nil {
		t.Fatal(err)
	}
	a := &Account{Agent: "zcode", User: "u@example.com"}
	if _, ok := RevokedSignIn(a, revokedReq(t, "a")); ok {
		t.Fatal("marked before anything was refused")
	}
	NoteRevokedSignIn(context.Background(), a, revokedReq(t, "a"))
	if why, ok := RevokedSignIn(a, revokedReq(t, "a")); !ok || !strings.Contains(why, "sign in again") {
		t.Fatalf("not marked: %q %v", why, ok)
	}
	if _, ok := RevokedSignIn(a, revokedReq(t, "b")); ok {
		t.Fatal("another credential of the account is held too")
	}
	if got := sideLapsed(t, "zcode", "u@example.com"); !strings.Contains(got, "sign in again") {
		t.Fatalf("the row isn't said signed out: %q", got)
	}
	// a restart reads it from logins.json
	resetRevoked()
	if _, ok := RevokedSignIn(a, revokedReq(t, "a")); !ok {
		t.Fatal("the mark is gone after a restart")
	}

	// served (its sign-in mended elsewhere): cleared, in logins.json too
	ServedSignIn(a)
	if _, ok := RevokedSignIn(a, revokedReq(t, "a")); ok {
		t.Fatal("still held after a request was served")
	}
	resetRevoked()
	if _, ok := RevokedSignIn(a, revokedReq(t, "a")); ok {
		t.Fatal("held again after a restart")
	}
	if got := sideLapsed(t, "zcode", "u@example.com"); got != "" {
		t.Fatalf("still said signed out: %q", got)
	}

	// marked again, then signed in anew in uni-switch: the row is clear
	NoteRevokedSignIn(context.Background(), a, revokedReq(t, "a"))
	if err := addSideLogin(savedLogin{Agent: "zcode", User: "u@example.com", Auth: json.RawMessage(`{"token":"c"}`)}, "", func(savedLogin) {}); err != nil {
		t.Fatal(err)
	}
	if got := sideLapsed(t, "zcode", "u@example.com"); got != "" {
		t.Fatalf("signed in again, still said signed out: %q", got)
	}
}

// Claude's, Copilot's and a plugin's accounts keep their own marks.
func TestRevokedSignInLeavesOthersMarks(t *testing.T) {
	for _, a := range []*Account{nil, {Agent: "claude", User: "x"}, {Agent: "copilot", User: "x"}, {Agent: "codex"}} {
		if RevokedAccount(a) {
			t.Errorf("%+v marked here", a)
		}
	}
	if !RevokedAccount(&Account{Agent: "codex", User: "x"}) {
		t.Error("a ChatGPT account isn't marked")
	}
}

// A saved ChatGPT account is refreshed before it is marked, as Codex does
// on a 401: a refresh that goes through is a new sign-in. A refresh refused
// marks it, and the account's row says so.
func TestRevokedCodexRefreshedFirst(t *testing.T) {
	home := signIn(t)
	resetRevoked()
	t.Cleanup(resetRevoked)
	codexSignIn(t, home, "old@example.com", "r-old")
	rememberLogins(true)
	codexSignIn(t, home, "work@example.com", "r-work")
	rememberLogins(true)
	refuse := false
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if refuse {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid_grant","error_description":"refresh token has been revoked"}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"access_token": "fresh-old", "refresh_token": "r-old-2"})
	}))
	defer fake.Close()
	old := codexTokenURL
	codexTokenURL = fake.URL
	t.Cleanup(func() { codexTokenURL = old })

	if err := SetLoginOn("codex", "old@example.com", true); err != nil {
		t.Fatal(err)
	}
	p, err := Find("codex")
	if err != nil {
		t.Fatal(err)
	}
	also := p.AlsoOn()
	if len(also) != 1 || also[0].Account.User != "old@example.com" || also[0].Account.token == nil {
		t.Fatalf("not the saved account: %+v", also)
	}
	a := also[0].Account
	lapsed := func() string {
		for _, l := range Logins("codex") {
			if l.User == "old@example.com" {
				return l.Lapsed
			}
		}
		t.Fatal("old@ isn't listed")
		return ""
	}

	NoteRevokedSignIn(context.Background(), a, revokedReq(t, "stale"))
	if _, ok := RevokedSignIn(a, revokedReq(t, "stale")); ok {
		t.Fatal("marked although its refresh went through")
	}
	if got := lapsed(); got != "" {
		t.Fatalf("refreshed, still said signed out: %q", got)
	}

	refuse = true
	NoteRevokedSignIn(context.Background(), a, revokedReq(t, "fresh-old"))
	if _, ok := RevokedSignIn(a, revokedReq(t, "fresh-old")); !ok {
		t.Fatal("its refresh refused, the account isn't marked")
	}
	if got := lapsed(); !strings.Contains(got, "sign in again") {
		t.Fatalf("not said signed out: %q", got)
	}
}

// The account's record written again with the same sign-in (as each time
// Codex's auth.json is read) keeps the mark; a new sign-in drops it.
func TestUpsertLoginKeepsRevokedMarkForTheSameSignIn(t *testing.T) {
	ls := []savedLogin{{Agent: "codex", User: "x@example.com", Auth: json.RawMessage("{\n  \"a\": 1\n}"), Lapsed: revokedText("x@example.com"), Refused: revokedPrefix + "m"}}
	same := upsertLogin(append([]savedLogin(nil), ls...), savedLogin{Agent: "codex", User: "x@example.com", Auth: json.RawMessage(`{"a":1}`)})
	if len(same) != 1 || same[0].Refused != revokedPrefix+"m" || same[0].Lapsed == "" {
		t.Fatalf("the same sign-in lost its mark: %+v", same)
	}
	anew := upsertLogin(append([]savedLogin(nil), ls...), savedLogin{Agent: "codex", User: "x@example.com", Auth: json.RawMessage(`{"a":2}`)})
	if len(anew) != 1 || anew[0].Refused != "" || anew[0].Lapsed != "" {
		t.Fatalf("a new sign-in kept the mark: %+v", anew)
	}
}
