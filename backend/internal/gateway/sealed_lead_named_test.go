package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// chatOnlyOn is a provider serving Chat Completions only, as the Qoder
// plugin does: it neither seals a subagent's task nor opens one. It
// counts the requests it is sent.
func chatOnlyOn(t *testing.T, id string) *int {
	t.Helper()
	n := new(int)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		*n++
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n"+
			"data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: id, Name: id, Key: "k", Chat: up.URL + "/v1", Models: []string{"dfmodel"}}); err != nil {
		t.Fatal(err)
	}
	return n
}

// plugins#70 (yygutn): a Codex lead first on a Qoder model, then on one of
// Codex's own (answered by the ChatGPT account, which uni-switch keeps no
// record of), spawns a subagent set to the Qoder model. The task the
// ChatGPT backend sealed is still turned away before Qoder is asked, but
// the lead is not said to have been answered, and the task sealed, by
// Qoder: Qoder seals nothing.
func TestSealedTaskNotPinnedOnALeadsEarlierChatProvider(t *testing.T) {
	fresh(t)
	chatgpt(t, func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(`data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[]}}`))
	})
	asked := chatOnlyOn(t, "qdr")
	s := New()
	if rec := codexSubTurn(s, "qdr/dfmodel", "lead-1", "", leadAsk); rec.Code != 200 {
		t.Fatalf("lead on qdr: %d %s", rec.Code, rec.Body.String())
	}
	if rec := codexSubTurn(s, "gpt-5.5", "lead-1", "", leadAsk); rec.Code != 200 {
		t.Fatalf("lead on ChatGPT: %d %s", rec.Code, rec.Body.String())
	}
	rec := codexSubTurn(s, "qdr/dfmodel", "worker-1", "lead-1", sealedHandoff)
	body := rec.Body.String()
	if rec.Code != 400 || *asked != 1 || strings.Contains(body, "gAAAAA") {
		t.Fatalf("subagent: %d %s (qdr asked %d times)", rec.Code, body, *asked)
	}
	if strings.Contains(body, "(qdr)") || strings.Contains(body, "use a model on qdr") ||
		!strings.Contains(body, "sealed by the ChatGPT backend, which answered the agent that sent it") ||
		!strings.Contains(body, "qdr/dfmodel can't. qdr answered the lead's earlier turns") {
		t.Fatalf("the refusal names qdr as the lead's sealer: %s", body)
	}
}
