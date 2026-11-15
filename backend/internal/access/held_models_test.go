package access

import (
	"slices"
	"testing"
)

// A key that names accounts and no model may use only its accounts'
// providers' models (Linka on Discord); one that lists models keeps
// them, and one that lists neither uses every model.
func TestHeldModels(t *testing.T) {
	for _, c := range []struct {
		models, accounts, want []string
	}{
		{nil, nil, nil},
		{[]string{"openai/gpt-5"}, []string{"codex/acct-1"}, []string{"openai/gpt-5"}},
		{nil, []string{"codex/acct-1", "CODEX/acct-2", "relay/abc"}, []string{"codex/*", "relay/*"}},
	} {
		if got := HeldModels(c.models, c.accounts); !slices.Equal(got, c.want) {
			t.Errorf("HeldModels(%v, %v) = %v, want %v", c.models, c.accounts, got, c.want)
		}
	}
	who := Identity{Models: HeldModels(nil, []string{"codex/acct-1"}), Accounts: []string{"codex/acct-1"}}
	if !who.Allows("codex/gpt-5.5") || who.Allows("anthropic/claude-opus-4-8") || who.Allows("group/any") {
		t.Fatal("an account-only key's models", who.Models)
	}
}
