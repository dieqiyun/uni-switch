package tui

import (
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// An account whose every sign-in in use was refused reads as signed out
// on the Providers page, as uni-switch providers and the app say it (LeeHom on
// Discord), not as a green signed-in account.
func TestProvidersSaySignedOut(t *testing.T) {
	home(t)
	m := model{w: 160, h: 40}
	m.provs = []provider.Provider{
		{ID: "claude", Name: "Claude Code", Account: &provider.Account{User: "two@example.com"}},
		{ID: "codex", Name: "Codex", Account: &provider.Account{User: "one@example.com"}},
	}
	signedOut = func(p provider.Provider) bool { return p.ID == "claude" }
	t.Cleanup(func() { signedOut = provider.Provider.SignedOut })
	var claude, codex string
	for _, l := range strings.Split(m.viewProviders(), "\n") {
		switch {
		case strings.Contains(l, "Claude Code"):
			claude = l
		case strings.Contains(l, "Codex"):
			codex = l
		}
	}
	if !strings.Contains(claude, "○ two@example.com is signed out") {
		t.Fatalf("signed-out row: %q", claude)
	}
	if !strings.Contains(codex, "● one@example.com") || strings.Contains(codex, "signed out") {
		t.Fatalf("signed-in row: %q", codex)
	}
}
