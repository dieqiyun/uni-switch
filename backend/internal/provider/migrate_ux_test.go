package provider

import (
	"encoding/json"

	"strings"
	"testing"
)

func setFakeLogins(t *testing.T, ls ...savedLogin) {
	t.Helper()
	loginsMu.Lock()
	defer loginsMu.Unlock()
	if err := writeLogins(ls); err != nil {
		t.Fatal(err)
	}
}

// A Zed account's list goes to the plugin with it, and comes back with it:
// the plugin serves the account's own models until it reads them, not the
// families it declares for a sign-in with none.
func TestZedMoveCarriesItsModels(t *testing.T) {
	claudeHome(t)
	list := `{"models":[{"provider":"anthropic","id":"claude-sonnet-4-5","display_name":"Claude Sonnet 4.5"}]}`
	setFakeLogins(t, savedLogin{Agent: "zed", User: "z@example.com", On: true, First: true,
		Auth: []byte(`{"userId":"u1","accessToken":"a","models":` + list + `}`)})
	out, err := movers["zed"].out()
	if err != nil || len(out) != 1 {
		t.Fatal(out, err)
	}
	var r struct {
		Models json.RawMessage `json:"models"`
	}
	if json.Unmarshal([]byte(str(out[0].Auth["refresh"])), &r) != nil || !strings.Contains(string(r.Models), "claude-sonnet-4-5") {
		t.Fatalf("the refresh carries no list: %v", out[0].Auth["refresh"])
	}
	ls, _, err := movers["zed"].back(nil, "z@example.com", out[0].Auth)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := zedSaved(ls[0])
	if string(c.Models) != list {
		t.Fatalf("back without its list: %s", c.Models)
	}
}
