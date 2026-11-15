package cli

import (
	"strings"
	"testing"
)

// uni-switch provider set balance.method= takes GET or POST, in any case, and
// keeps GET as the unset default; any other method is refused and leaves
// the saved one alone (#2368).
func TestProviderSetBalanceMethod(t *testing.T) {
	groupsHome(t)
	for _, c := range []struct{ v, want string }{
		{"POST", "POST"}, {"get", ""}, {" post ", "POST"}, {"", ""},
	} {
		if _, err := printed(t, func() error {
			return providerCmd([]string{"provider", "set", "a", "balance=https://relay.example.com/api/check-balance", "balance.method=" + c.v})
		}); err != nil {
			t.Fatalf("%q: %v", c.v, err)
		}
		if got := mustFind(t, "a").BalanceMethod; got != c.want {
			t.Fatalf("balance.method=%q kept %q, want %q", c.v, got, c.want)
		}
	}
	if _, err := printed(t, func() error { return providerCmd([]string{"provider", "set", "a", "balance.method=POST"}) }); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"PUT", "DELETE", "POST\r\nX: y"} {
		err := providerCmd([]string{"provider", "set", "a", "balance.method=" + v})
		if err == nil || !strings.Contains(err.Error(), "GET|POST") {
			t.Errorf("balance.method=%q: %v", v, err)
		}
		if got := mustFind(t, "a").BalanceMethod; got != "POST" {
			t.Errorf("a refused %q left %q", v, got)
		}
	}
}
