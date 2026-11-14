package gateway

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// antigravitySuspended is Google's answer for an Antigravity account it
// suspended, as bestony's agent showed it on Discord (#feedback, 11-06):
// "Error 403, Message: Antigravity: This service has been disabled in this
// account for violation of Terms of Service. Please submit an appeal to
// continue using this product., Status: PERMISSION_DENIED".
const antigravitySuspended = `{"error":{"code":403,"message":"This service has been disabled in this account for violation of Terms of Service. Please submit an appeal to continue using this product.","status":"PERMISSION_DENIED"}}`

// Only Google's suspension of a Google sign-in is read so: another 403 from
// Antigravity, and the same words from a key's relay, fail as before.
func TestAntigravitySuspendedIsGooglesAlone(t *testing.T) {
	ag := candidate{p: provider.Provider{ID: "antigravity", Account: &provider.Account{Agent: "antigravity", User: "u"}}}
	gem := candidate{p: provider.Provider{ID: "gemini", Account: &provider.Account{Agent: "gemini", User: "u"}}}
	key := candidate{p: provider.Provider{ID: "relay", Key: "k"}}
	said := "Antigravity: This service has been disabled in this account for violation of Terms of Service. Please submit an appeal to continue using this product."
	for _, c := range []struct {
		c      candidate
		status int
		body   string
		want   string
	}{
		{ag, 403, antigravitySuspended, failRestricted},
		{ag, 403, said, failRestricted},
		{gem, 403, antigravitySuspended, failRestricted},
		{ag, 403, `{"error":{"code":403,"message":"The caller does not have permission","status":"PERMISSION_DENIED"}}`, failOther},
		{ag, 400, antigravitySuspended, failOther},
		{key, 403, antigravitySuspended, failOther},
	} {
		if got := failureOf(c.c, c.status, []byte(c.body)); got != c.want {
			t.Errorf("%s %d %s: %s, want %s", c.c.p.ID, c.status, c.body, got, c.want)
		}
	}
}
