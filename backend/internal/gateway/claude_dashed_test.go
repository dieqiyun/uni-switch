package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// misteer_ti. on Discord: Claude Code reads a relay's claude-opus-4.6 as
// Claude Opus 4, so uni-switch gives it relay/claude-opus-4-6-us. The gateway
// takes that dashed spelling back to the model the relay serves, its
// effort and [1m] mark kept; a model served by the dashed name itself is
// that one, and another vendor's id is never respelled.
func TestClaudeDashedIDsReachTheDottedModel(t *testing.T) {
	fresh(t)
	reply := func(id string) *fake {
		return &fake{t: t, ctype: "application/json", reply: `{"id":"` + id + `","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`}
	}
	relay, both := reply("from-relay"), reply("from-both")
	for _, x := range []struct {
		p provider.Provider
		f *fake
	}{
		{provider.Provider{ID: "relay", Name: "Relay", Key: "k", Models: []string{"claude-opus-4.6-us", "claude-haiku-4.5", "glm-4.6"}}, relay},
		{provider.Provider{ID: "both", Name: "Both", Key: "k", Models: []string{"claude-opus-4.6", "claude-opus-4-6"}}, both},
	} {
		up := httptest.NewServer(x.f)
		t.Cleanup(up.Close)
		x.p.Anthropic = up.URL
		if err := provider.Save(x.p); err != nil {
			t.Fatal(err)
		}
	}
	h := New().Handler()
	do := func(path, model string) *httptest.ResponseRecorder {
		body := `{"model":"` + model + `","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+Token)
		r.Header.Set("User-Agent", "claude-cli/2.1.296 (external, cli)")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	sent := func(f *fake) string {
		var b struct{ Model string }
		json.Unmarshal(f.got, &b)
		return b.Model
	}
	for _, c := range []struct {
		asked string
		f     *fake
		want  string
	}{
		{"relay/claude-opus-4-6-us", relay, "claude-opus-4.6-us"},
		{"relay/claude-opus-4-6-us[1m]", relay, "claude-opus-4.6-us"},
		{"relay/claude-opus-4-6-us:high", relay, "claude-opus-4.6-us"},
		{"relay/claude-haiku-4-5", relay, "claude-haiku-4.5"},
		{"relay/claude-opus-4.6-us", relay, "claude-opus-4.6-us"},
		// served by the dashed name itself: that one
		{"both/claude-opus-4-6", both, "claude-opus-4-6"},
		{"both/claude-opus-4.6", both, "claude-opus-4.6"},
		// another vendor's id is as asked
		{"relay/glm-4-6", relay, "glm-4-6"},
	} {
		if w := do("/v1/messages", c.asked); w.Code != 200 || sent(c.f) != c.want {
			t.Fatalf("%s: %d %s, sent %q, want %q", c.asked, w.Code, w.Body.String(), sent(c.f), c.want)
		}
	}
	do("/v1/messages", "relay/claude-opus-4-6-us")
	if rec := lastUsage(t); rec.Provider != "relay" || rec.Requested != "relay/claude-opus-4.6-us" {
		t.Fatalf("recorded as %+v", rec)
	}
	if w := do("/v1/messages/count_tokens", "relay/claude-opus-4-6-us"); w.Code != 200 {
		t.Fatalf("count_tokens: %d %s", w.Code, w.Body.String())
	}
}
