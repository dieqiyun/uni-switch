package provider

import (
	"testing"
)

// An account's stable id (#905) is set once its login is written and kept
// through a rename: a gateway key held to accounts names it by that, not
// by a name that moves. Until the logins are written, one made of the
// name stands in.
func TestLoginIDStable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := writeLogins([]savedLogin{{Agent: "codex", User: "me@example.com"}, {Agent: "codex", User: "spare@example.com", On: true}}); err != nil {
		t.Fatal(err)
	}
	ls := readLogins()
	if ls[0].ID == "" || ls[1].ID == "" || ls[0].ID == ls[1].ID {
		t.Fatalf("written without ids: %+v", ls)
	}
	id := ls[0].ID
	ls[0].User = "renamed@example.com"
	if err := writeLogins(ls); err != nil {
		t.Fatal(err)
	}
	if got := LoginID("codex", "renamed@example.com"); got != id {
		t.Fatalf("a rename moved the id: %s, was %s", got, id)
	}
	// a name no account has is one of no account's: another id, matching
	// nothing a candidate carries
	if got := LoginID("codex", "other@example.com"); got == id {
		t.Fatal("another account had the same id")
	}
}
