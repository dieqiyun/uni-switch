package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// sealedWorkerMessage is what Codex (codex-rs protocol.rs,
// InterAgentCommunication::to_model_input_item) puts in its lead's input
// when a subagent on one of Codex's own models sends it a message with
// send_message: the ChatGPT backend sealed the call's arguments, so the
// message goes as encrypted_content, under a plain header.
const sealedWorkerMessage = `{"type":"agent_message","author":"/root/worker","recipient":"/root","content":[{"type":"input_text","text":"Message Type: MESSAGE\nTask name: /root\nSender: /root/worker\nPayload:\n"},{"type":"encrypted_content","encrypted_content":"gAAAAATest_worker_message=="}]}`

// plainTask is a lead's spawn_agent message as Codex hands it to a subagent
// when uni-switch served the lead: plain text, encrypted_function_args [].
const plainTask = `{"type":"agent_message","author":"/root","recipient":"/root/helper","content":[{"type":"input_text","text":"read the README"}]}`

// chatRecorded is a Chat Completions provider (an API key on an
// OpenAI-compatible endpoint, laacmilan's "token"), keeping every request.
type chatRecorded struct {
	mu  sync.Mutex
	got []string
}

func (f *chatRecorded) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.got...)
}

func chatRecordedOn(t *testing.T, id, model string) *chatRecorded {
	t.Helper()
	f := &chatRecorded{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.got = append(f.got, string(b))
		f.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n"+
			"data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: id, Name: id, Key: "k", Chat: up.URL + "/v1", Models: []string{model}}); err != nil {
		t.Fatal(err)
	}
	return f
}

// #1613 (laacmilan): Codex's lead on token/gpt-6.1-sol, a provider on Chat
// Completions, and a subagent on one of Codex's own models, answered by
// the ChatGPT account. The subagent's send_message to the lead is sealed
// by the ChatGPT backend. The lead's next turn went nowhere: uni-switch turned
// it away as a "subagent's task" sealed for a lead that "has since been on
// one of Codex's own models", which it never was. A sealed message (not a
// task) the model can't open is left out, said so in its place, and the
// lead goes on on token.
func TestSealedMessageToALeadOnAChatProviderIsLeftOut(t *testing.T) {
	fresh(t)
	var native int
	chatgpt(t, func(w http.ResponseWriter, r *http.Request) { io.ReadAll(r.Body); native++ })
	token := chatRecordedOn(t, "token", "gpt-6.1-sol")
	s := New()
	if rec := codexSubTurn(s, "token/gpt-6.1-sol", "lead-1", "", leadAsk); rec.Code != 200 {
		t.Fatalf("lead's first turn: %d %s", rec.Code, rec.Body.String())
	}
	rec := codexSubTurn(s, "token/gpt-6.1-sol", "lead-1", "", leadAsk+`,`+sealedWorkerMessage)
	if rec.Code != 200 {
		t.Fatalf("lead's turn with its subagent's sealed message: %d %s", rec.Code, rec.Body.String())
	}
	got := token.requests()
	if len(got) != 2 || native != 0 {
		t.Fatalf("token was asked %d times, the ChatGPT backend %d", len(got), native)
	}
	if strings.Contains(got[1], "gAAAAA") || !strings.Contains(got[1], "/root/worker") || !strings.Contains(got[1], "left out") {
		t.Fatalf("the lead's request to token: %s", got[1])
	}
}

// The same for a subagent on token whose task is plain (its lead on
// token) and that is sent a sealed message by an agent on one of Codex's
// own models: the message is left out, the task kept.
func TestSealedMessageToASubagentOnAChatProviderIsLeftOut(t *testing.T) {
	fresh(t)
	chatgpt(t, func(w http.ResponseWriter, r *http.Request) { io.ReadAll(r.Body) })
	token := chatRecordedOn(t, "token", "gpt-6.1-sol")
	s := New()
	if rec := codexSubTurn(s, "token/gpt-6.1-sol", "lead-1", "", leadAsk); rec.Code != 200 {
		t.Fatalf("lead: %d %s", rec.Code, rec.Body.String())
	}
	msg := strings.NewReplacer(`"recipient":"/root"`, `"recipient":"/root/helper"`, `Task name: /root\n`, `Task name: /root/helper\n`).Replace(sealedWorkerMessage)
	rec := codexSubTurn(s, "token/gpt-6.1-sol", "helper-1", "lead-1", plainTask+`,`+msg)
	if rec.Code != 200 {
		t.Fatalf("subagent: %d %s", rec.Code, rec.Body.String())
	}
	got := token.requests()
	if len(got) != 2 || strings.Contains(got[1], "gAAAAA") || !strings.Contains(got[1], "read the README") || !strings.Contains(got[1], "left out") {
		t.Fatalf("token was sent %d requests; the subagent's: %v", len(got), got[len(got)-1:])
	}
}

// A sealed task is still turned away: without it the subagent has
// nothing to do. Only a message is left out.
func TestSealedTaskOnAChatProviderStillTurnedAway(t *testing.T) {
	fresh(t)
	chatgpt(t, func(w http.ResponseWriter, r *http.Request) { io.ReadAll(r.Body) })
	token := chatRecordedOn(t, "token", "gpt-6.1-sol")
	s := New()
	if rec := codexSubTurn(s, "token/gpt-6.1-sol", "lead-1", "", leadAsk); rec.Code != 200 {
		t.Fatalf("lead: %d %s", rec.Code, rec.Body.String())
	}
	rec := codexSubTurn(s, "token/gpt-6.1-sol", "worker-1", "lead-1", sealedHandoff)
	if rec.Code != 400 || len(token.requests()) != 1 {
		t.Fatalf("subagent: %d %s (token asked %d times)", rec.Code, rec.Body.String(), len(token.requests()))
	}
	if body := rec.Body.String(); strings.Contains(body, "the lead has since been on") || !strings.Contains(body, "token/gpt-6.1-sol can't") {
		t.Fatalf("the refusal: %s", body)
	}
}
