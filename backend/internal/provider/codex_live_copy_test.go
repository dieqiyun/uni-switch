package provider

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// A saved ChatGPT account switched in to Codex is one sign-in in two
// places: logins.json's copy and Codex's auth.json hold one refresh token.
// A refresh asked for through the copy (an account in use beside Codex's
// own when the switch came, the backend calling its token expired) spent
// that token under Codex, and auth.json was left with a dead one. The
// Codex app of an edu workspace reads its workspace's settings with it at
// start, and stayed paused on "无法加载组织设置" (万贤凯 on Discord).
func TestSavedRefreshOfCodexOwnRenewsCodexOwn(t *testing.T) {
	home, refreshes, _ := codexExpiredFakes(t, staleExpired, false)
	if err := SwitchLogin("codex", "me@example.com"); err != nil {
		t.Fatal(err)
	}
	var live codexAuth
	readJSON(filepath.Join(home, ".codex", "auth.json"), &live)
	if live.Tokens.RefreshToken != "r" {
		t.Fatalf("switched in: %+v", live.Tokens)
	}

	// as the provider made for me@ while work@ was Codex's asks, held
	// over the switch
	tok, accountID, err := renewSavedLoginFrom(context.Background(), "codex", "me@example.com", live.Tokens.AccessToken)
	if err != nil || tok != "fresh-r" || accountID != "acct-1" {
		t.Fatalf("renewed: %q %q %v", tok, accountID, err)
	}
	readJSON(filepath.Join(home, ".codex", "auth.json"), &live)
	if live.Tokens.AccessToken != "fresh-r" || live.Tokens.RefreshToken != "r-2" {
		t.Fatalf("Codex's auth.json kept the refresh token spent: %+v", live.Tokens)
	}
	if got := refreshes(); len(got) != 1 || got[0] != "r" {
		t.Fatalf("refreshed with %q, want r once", got)
	}

	// and by keep-alive's force: Codex's own is fresh, so nothing is sent
	// that would spend it
	if _, _, err := renewSavedLogin(context.Background(), "codex", "me@example.com", true); err != nil {
		t.Fatal(err)
	}
	if got := refreshes(); len(got) != 1 {
		t.Fatalf("refreshed again with %q", got)
	}

	// work@, the one switched away from, still renews its own copy, and
	// only that
	for _, l := range readLogins() {
		if l.User == "work@example.com" && !strings.Contains(string(l.Auth), `"r-work"`) {
			t.Fatalf("work@'s copy: %s", l.Auth)
		}
	}
	if tok, _, err := renewSavedLogin(context.Background(), "codex", "work@example.com", true); err != nil || tok != "fresh-r-work" {
		t.Fatalf("work@: %q %v", tok, err)
	}
	readJSON(filepath.Join(home, ".codex", "auth.json"), &live)
	if live.Tokens.RefreshToken != "r-2" {
		t.Fatalf("work@'s refresh reached auth.json: %+v", live.Tokens)
	}
}
