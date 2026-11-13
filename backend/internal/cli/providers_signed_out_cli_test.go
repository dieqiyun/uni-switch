package cli

import (
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// An account whose every sign-in in use was refused is listed signed out,
// to be signed in again, never "signed in as" (LeeHom on Discord: two
// Claude Pro accounts gone from the list, then "needs sign in again").
func TestProvidersSaySignedOut(t *testing.T) {
	out := map[string]bool{"claude": true}
	signedOut = func(p provider.Provider) bool { return out[p.ID] }
	t.Cleanup(func() { signedOut = provider.Provider.SignedOut })
	gone := keyColumn(provider.Provider{ID: "claude", Account: &provider.Account{User: "two@example.com"}})
	if !strings.Contains(gone, "two@example.com is signed out") || !strings.Contains(gone, "sign in again") || strings.Contains(gone, "signed in as") {
		t.Fatalf("signed-out row: %q", gone)
	}
	in := keyColumn(provider.Provider{ID: "codex", Account: &provider.Account{User: "one@example.com"}})
	if !strings.Contains(in, "signed in as one@example.com") {
		t.Fatalf("signed-in row: %q", in)
	}
}
