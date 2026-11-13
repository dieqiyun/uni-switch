package gateway

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// WorkBuddy's security policy turning away an agent's system prompt (Claude
// Code's own) "from an unapproved channel", whichever account it goes to:
// the next account is asked, and the one that answered so doesn't rest —
// before, lemon's two WorkBuddy AI accounts both rested over one agent's
// chat, for every agent (Discord). Told as the agent's prompt, not a
// failure of the account.
func TestPromptRefusalRestsNobody(t *testing.T) {
	refuse := func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		io.WriteString(w, `{"code":11128,"msg":"Illegal API invocation from an unapproved channel"}`)
	}
	for _, x := range []struct {
		name string
		also []string // the second account refuses too
	}{
		{"next answers", nil},
		{"nobody left", []string{"acct-2"}},
	} {
		t.Run(x.name, func(t *testing.T) {
			codexSignedIn(t, "spare@example.com")
			if err := provider.SetRouting("codex", provider.Ordered); err != nil {
				t.Fatal(err)
			}
			var tried []string
			chatgptRefusing(t, &tried, refuse, x.also...)
			s := New()
			code, body := codexAskOn(s, `{"model":"gpt-5.5","stream":true,"input":"ping"}`)
			if strings.Join(tried, ",") != "acct-1,acct-2" {
				t.Fatalf("tried %v", tried)
			}
			r := lastRoute(s)
			if len(r.Tries) != 2 || r.Tries[0].Fail != failPrompt || r.Tries[0].Rest != nil {
				t.Fatalf("tries: %+v", r.Tries)
			}
			if x.also == nil {
				if code != 200 || !strings.Contains(body, "pong") || r.Tries[1].Status != 200 {
					t.Fatalf("status %d: %s", code, body)
				}
			} else if code != 400 || !strings.Contains(body, "unapproved channel") || r.Tries[1].Fail != failPrompt || r.Tries[1].Rest != nil {
				t.Fatalf("status %d: %s; tries %+v", code, body, r.Tries)
			}
			for _, c := range []string{"codex", "codex@me@example.com", "codex@spare@example.com"} {
				if _, ok := restOf(c); ok {
					t.Fatalf("%s set aside by a prompt the vendor turns away", c)
				}
			}
		})
	}
}
