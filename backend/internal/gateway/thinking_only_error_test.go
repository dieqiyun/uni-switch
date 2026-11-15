package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A reply that thinks and then fails, with nothing said: Cursor's plugin
// ends such a turn with "an empty reply". A client streaming it got that
// error; one asking for the whole reply got a 200 with no content and no
// usage, as if the model had answered nothing, and didn't ask again.
func TestThinkingThenErrorIsAnError(t *testing.T) {
	fresh(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`+"\n\n")
		io.WriteString(w, `data: {"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"reasoning_content":"thinking it over"}}]}`+"\n\n")
		io.WriteString(w, `data: {"error":{"message":"an empty reply","code":502}}`+"\n\n")
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "go", Name: "OpenCode Go", Key: "k", Models: []string{"m"}, Chat: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	// spoken to in Messages, so the reply is translated whole
	code, body := post(t, "/v1/messages", `{"model":"go/m","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`)
	if code < 400 || !strings.Contains(body, "an empty reply") {
		t.Errorf("thinking and then an error answered %d %s, not the error", code, body)
	}
}
