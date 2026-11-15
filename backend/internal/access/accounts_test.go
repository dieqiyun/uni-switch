package access

import (
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A key's account list as it is kept (#905): each entry a provider's
// account or key it has now — so a typo can't quietly hold a key to
// nothing — trimmed, told apart in lower case as accounts are, each once.
// An account's stable id, and a name resolving to it, is exercised
// against a real sign-in in the gateway's tests; a key's fingerprint is
// one here.
func TestCleanAccounts(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Chat: "http://127.0.0.1:1/v1", Keys: []provider.KeyAccount{{Key: "sk-1", Name: "First"}, {Key: "sk-2"}}}); err != nil {
		t.Fatal(err)
	}
	k1, k2 := provider.KeyID("sk-1"), provider.KeyID("sk-2")
	for _, c := range []struct {
		name string
		in   []string
		want []string
		bad  string
	}{
		{"an empty list is every account", nil, nil, ""},
		{"a key by its fingerprint", []string{"relay/" + k1}, []string{"relay/" + k1}, ""},
		{"another case of it is the same one", []string{" relay/" + strings.ToUpper(k1) + " ", "relay/" + k1}, []string{"relay/" + k1}, ""},
		{"a key by the name it is shown by, kept by its fingerprint", []string{"relay/First", "relay/" + k2}, []string{"relay/" + k1, "relay/" + k2}, ""},
		{"an account with no provider", []string{"sk-1"}, nil, "<provider>/<account>"},
		{"a provider there is none of", []string{"nope/sk-1"}, nil, "no provider"},
		{"a key the provider has not", []string{"relay/" + provider.KeyID("sk-9")}, nil, "has no key"},
		{"an account the provider has none of", []string{"relay/me@example.com"}, nil, "has no key"},
	} {
		out, err := CleanAccounts(c.in)
		if c.bad != "" {
			if err == nil || !strings.Contains(err.Error(), c.bad) {
				t.Errorf("%s: said %v, want %q in it", c.name, err, c.bad)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !slices.Equal(out, c.want) {
			t.Errorf("%s: kept %v, want %v", c.name, out, c.want)
		}
	}
	// matching is in lower case, both sides; a provider the list names no
	// account of, the key uses as it always did
	who := Identity{Accounts: []string{"relay/" + strings.ToUpper(k1)}}
	if !who.AllowsAccount("relay", k1) || who.AllowsAccount("relay", k2) {
		t.Fatal("told apart in lower case")
	}
	if !who.AllowsAccount("other", k1) {
		t.Fatal("a provider the list names no account of was held")
	}
	if !(Identity{}).AllowsAccount("relay", k1) {
		t.Fatal("a key that lists none uses every account")
	}
}
