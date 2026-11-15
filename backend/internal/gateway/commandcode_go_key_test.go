package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A Go plan's key added as a Command Code API-key provider is refused by
// the Provider API (#969); the error the agent sees says where the key
// works, on every protocol the preset serves.
func TestCommandCodeGoKeySaysWhereItWorks(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		io.WriteString(w, `{"error":{"type":"forbidden","message":"Your Go plan doesn't include API access. Upgrade to Provider or higher at https://commandcode.ai/billing to use these endpoints."}}`)
	}))
	defer up.Close()
	if err := provider.Save(provider.Provider{ID: "commandcode", Name: "Command Code", Key: "k", Models: []string{"m1"},
		Chat: up.URL + "/provider/v1", Responses: up.URL + "/provider/v1", Anthropic: up.URL + "/provider"}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct{ path, body string }{
		{"/v1/chat/completions", `{"model":"commandcode/m1","messages":[{"role":"user","content":"hi"}]}`},
		{"/v1/messages", `{"model":"commandcode/m1","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`},
		{"/v1/responses", `{"model":"commandcode/m1","input":"hi"}`},
	} {
		forgetRouting()
		code, body := post(t, q.path, q.body)
		if code < 400 || !strings.Contains(body, "Go plan doesn't include API access") || !strings.Contains(body, "Plugins › Command Code") {
			t.Errorf("%s: %d %s", q.path, code, body)
		}
	}
}
