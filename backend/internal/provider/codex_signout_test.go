package provider

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Codex's only account is signed out when it is removed, as codex logout
// does (mamba on Discord); one with another saved signs Codex in to that
// one first (vincentzhang1_55530 on Discord: a Team account removed beside
// a Plus one left Codex at its sign-in screen); and ForgetAccount, which
// takes the one in use last, signs Codex out too.
func TestCodexSignOutLastAccount(t *testing.T) {
	home := signIn(t)
	auth := filepath.Join(home, ".codex", "auth.json")
	rememberLogins(true)
	var signedIn *SignedInError

	// another saved: the one signed in to goes, and Codex is signed in to
	// the other, its own sign-in back in auth.json
	codexSignIn(t, home, "work@example.com", "r-work")
	rememberLogins(true)
	if err := ForgetLogin("codex", "work@example.com"); err != nil {
		t.Fatalf("the one in use with another saved: %v", err)
	}
	live, ok := liveLogin("codex")
	if !ok || live.User != "me@example.com" {
		t.Fatalf("Codex signed in to %q (%v), want me@example.com", live.User, ok)
	}
	if b, _ := os.ReadFile(auth); strings.Contains(string(b), "r-work") {
		t.Fatalf("auth.json still the removed one's: %s", b)
	}
	rememberLogins(true)
	if ls := Logins("codex"); len(ls) != 1 || ls[0].User != "me@example.com" || !ls[0].Active {
		t.Fatalf("listed: %+v", ls)
	}

	// the one left is signed out
	if err := ForgetLogin("codex", "me@example.com"); err != nil {
		t.Fatalf("the only account: %v", err)
	}
	if _, err := os.Stat(auth); !os.IsNotExist(err) {
		t.Fatalf("auth.json still there: %v", err)
	}
	if ls := Logins("codex"); len(ls) != 0 {
		t.Fatalf("still listed: %+v", ls)
	}
	rememberLogins(true)
	if ls := Logins("codex"); len(ls) != 0 {
		t.Fatalf("remembered again: %+v", ls)
	}

	// signed in but not saved yet: signed out all the same
	codexSignIn(t, home, "new@example.com", "r-new")
	if err := ForgetLogin("codex", "new@example.com"); err != nil {
		t.Fatalf("unsaved: %v", err)
	}
	if _, err := os.Stat(auth); !os.IsNotExist(err) {
		t.Fatalf("auth.json still there: %v", err)
	}

	// an account not signed in to and not saved is still unknown
	if err := ForgetLogin("codex", "nobody@example.com"); err == nil || errors.As(err, &signedIn) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestForgetAccountSignsCodexOut(t *testing.T) {
	home := signIn(t)
	rememberLogins(true)
	codexSignIn(t, home, "work@example.com", "r-work")
	rememberLogins(true)
	if err := ForgetAccount("codex"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "auth.json")); !os.IsNotExist(err) {
		t.Fatalf("auth.json still there: %v", err)
	}
	if ls := Logins("codex"); len(ls) != 0 {
		t.Fatalf("saved still: %+v", ls)
	}
}
