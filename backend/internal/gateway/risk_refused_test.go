package gateway

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// riskBlocked answers as an account whose vendor's risk control blocked it,
// in uni-switch's words (provider.ZCodeRestrictedHint, as the ZCode plugin
// puts its 3012).
func riskBlocked(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	io.WriteString(w, `{"error":{"message":"blocked — `+provider.ZCodeRestrictedHint+`","type":"permission_error"}}`)
}

// With no other provider, the agent is told at once and the account
// rests, whether it was the only account or the provider had more; a
// refusal that isn't the vendor's risk control (a plain 403 alone) still
// goes on to the next account as before.
func TestRiskRefusalAloneIsToldAndRests(t *testing.T) {
	for _, x := range []struct {
		name   string
		spares []string
	}{{"lone account", nil}, {"two accounts", []string{"b@example.com"}}} {
		t.Run(x.name, func(t *testing.T) {
			codexSignedIn(t, x.spares...)
			if err := provider.SetRouting("codex", provider.Ordered); err != nil {
				t.Fatal(err)
			}
			var tried []string
			chatgptRefusing(t, &tried, riskBlocked)
			s := New()
			code, body := postAs(t, s, "", `{"model":"codex/gpt-5.5","messages":[{"role":"user","content":"hi"}]}`)
			if code != http.StatusForbidden || !strings.Contains(body, provider.ZCodeRestrictedHint) || strings.Join(tried, ",") != "acct-1" {
				t.Fatalf("%d %s; asked %v", code, body, tried)
			}
			r, ok := restOf("codex@me@example.com")
			if !ok || r.Why != failRestricted || r.By != "restricted" {
				t.Fatalf("the restricted account rests %+v (%v)", r, ok)
			}
		})
	}
	t.Run("a plain 403 goes on", func(t *testing.T) {
		codexSignedIn(t, "b@example.com")
		if err := provider.SetRouting("codex", provider.Ordered); err != nil {
			t.Fatal(err)
		}
		var tried []string
		chatgptRefusing(t, &tried, func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"error":{"message":"Forbidden"}}`)
		})
		if code, body := postAs(t, New(), "", `{"model":"codex/gpt-5.5","messages":[{"role":"user","content":"hi"}]}`); code != 200 || strings.Join(tried, ",") != "acct-1,acct-2" {
			t.Fatalf("%d %s; asked %v", code, body, tried)
		}
	})
}
