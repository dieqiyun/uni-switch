package provider

import (
	"context"
	"encoding/json"

	"strings"
	"testing"
)

func fakeToken(l savedLogin) string {
	var a struct{ Token string }
	_ = json.Unmarshal(l.Auth, &a)
	return a.Token
}

func fakeLogin(user, token string, first, on bool) savedLogin {
	return savedLogin{Agent: "fakeco", User: user, First: first, On: on, Auth: json.RawMessage(jsonText(map[string]string{"token": token}))}
}

func fakeSaved(t *testing.T) map[string]savedLogin {
	t.Helper()
	out := map[string]savedLogin{}
	for _, l := range readLogins() {
		if l.Agent == "fakeco" {
			out[l.User] = l
		}
	}
	return out
}

// A retired built-in shows no card of its own, even with an account of it
// still on this machine, unmoved, and signs in no more: an account signed
// in to there would be served by nothing.
func TestRetiredBuiltinQuiet(t *testing.T) {
	claudeHome(t)
	loginsMu.Lock()
	_ = writeLogins([]savedLogin{{Agent: "zed", User: "left@example.com", On: true, First: true, Auth: []byte(`{"userId":"u1","accessToken":"a"}`)}})
	loginsMu.Unlock()
	if !BuiltinRetired("zed") {
		t.Fatal("zed isn't retired")
	}
	for _, q := range fetchSubscriptionUsage(context.Background()) {
		if q.Provider == "zed" {
			t.Fatalf("a card of the retired built-in: %+v", q)
		}
	}
	if _, err := StartSignIn("zed"); err == nil || !strings.Contains(err.Error(), "plugin") {
		t.Fatalf("a sign-in to the retired built-in: %v", err)
	}
	if c := MoveCandidates(); len(c) != 1 || c[0].ID != "zed" || c[0].Accounts != 1 {
		t.Fatalf("the unmoved account isn't offered its move: %+v", c)
	}
}

// plugins#404 (Gruu6y): Grok moved onto its plugin, then the plugin, the
// provider and its sign-ins removed. migrations.json says "removed", but a
// login row of the built-in's left behind kept it a move candidate, and the
// Providers page said for good that its accounts hadn't moved yet, with a
// Move that would install the plugin again. Removed is the user's answer:
// no candidate, until the plugin is installed again (HandOver).
func TestRemovedPluginIsNoMoveCandidate(t *testing.T) {
	claudeHome(t)
	loginsMu.Lock()
	_ = writeLogins([]savedLogin{{Agent: "zed", User: "left@example.com", On: true, First: true, Auth: []byte(`{"userId":"u1","accessToken":"a"}`)}})
	loginsMu.Unlock()
	if c := MoveCandidates(); len(c) != 1 || c[0].ID != "zed" {
		t.Fatalf("the unmoved account isn't offered its move: %+v", c)
	}
	if err := setMigration("zed", func(m *Migration) {
		*m = Migration{State: MoveRemoved, Package: MovePackage("zed"), Installed: true}
	}); err != nil {
		t.Fatal(err)
	}
	if c := MoveCandidates(); len(c) != 0 {
		t.Fatalf("one whose plugin the user removed is still offered a move: %+v", c)
	}
	// one whose move failed still is, with why
	if err := setMigration("zed", func(m *Migration) { m.State, m.Err = MoveFailed, "boom" }); err != nil {
		t.Fatal(err)
	}
	if c := MoveCandidates(); len(c) != 1 || c[0].Error != "boom" {
		t.Fatalf("a failed move isn't offered again: %+v", c)
	}
}
