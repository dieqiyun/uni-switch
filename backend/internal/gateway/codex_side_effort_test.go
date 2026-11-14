package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// needsEffort is an upstream whose model turns away a request with no
// reasoning effort, or none, as cybercaius's did for Codex's side calls;
// refusal is what it answers then.
type needsEffort struct {
	mu      sync.Mutex
	got     []string
	refusal string
}

func (f *needsEffort) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.got = append(f.got, string(b))
	f.mu.Unlock()
	if e := f.effortOf(string(b)); e == "" || e == "none" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, f.refusal)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	if strings.HasSuffix(r.URL.Path, "/chat/completions") {
		io.WriteString(w, sse(`data: {"id":"c1","choices":[{"index":0,"delta":{"content":"{\"title\":\"Fix the build\"}"}}]}`,
			`data: {"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`, `data: [DONE]`))
		return
	}
	io.WriteString(w, sse(`data: {"type":"response.output_text.delta","delta":"{\"title\":\"Fix the build\"}"}`,
		`data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"{\"title\":\"Fix the build\"}"}]}]}}`))
}

// effortOf is the reasoning effort a Responses or Chat body asks for.
func (f *needsEffort) effortOf(body string) string {
	var v struct {
		ReasoningEffort string `json:"reasoning_effort"`
		Reasoning       *struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
	}
	_ = json.Unmarshal([]byte(body), &v)
	if v.Reasoning != nil {
		return v.Reasoning.Effort
	}
	return v.ReasoningEffort
}

func (f *needsEffort) efforts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, b := range f.got {
		out = append(out, f.effortOf(b))
	}
	return out
}

const effortRequiredBody = `{"error":{"message":"Reasoning effort is required for this model","type":"invalid_request_error","param":"reasoning.effort"}}`

// needsEffortOn is provider id serving m1 through f, on Responses or,
// with chat, on Chat Completions alone.
func needsEffortOn(t *testing.T, id string, chat bool, refusal string) *needsEffort {
	t.Helper()
	f := &needsEffort{refusal: refusal}
	up := httptest.NewServer(f)
	t.Cleanup(up.Close)
	p := provider.Provider{ID: id, Name: strings.ToUpper(id), Key: "sk-" + id, Models: []string{"m1"}, Responses: up.URL + "/v1"}
	if chat {
		p.Responses, p.Chat = "", up.URL+"/v1"
	}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	return f
}

// codexTitleOn is the Codex title Codex asks on model, the conversation's
// own: Codex sends it with no reasoning effort, as it does on any model
// but its own title model.
func codexTitleOn(s *Server, model string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	body := `{"model":"` + model + `","stream":true,"input":[` + leadAsk + `]}`
	req := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer chatgpt-token")
	req.Header.Set("chatgpt-account-id", "acct-1")
	req.Header.Set("session_id", "title-thread")
	req.Header.Set("x-codex-turn-metadata", `{"thread_source":"thread_title"}`)
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// cybercaius on Discord: Codex's side calls went with no effort to a
// model that needs one, which answered 400. With the model's levels known,
// a title is asked at its lowest on level from the start.
func TestCodexSideCallAskedAtAnEffortTheModelTakes(t *testing.T) {
	fresh(t)
	up := needsEffortOn(t, "fx", false, effortRequiredBody)
	if err := provider.SetModelEfforts("fx/m1", []string{"low", "medium", "high"}); err != nil {
		t.Fatal(err)
	}
	s := New()
	if rec := codexTitleOn(s, "fx/m1"); rec.Code != 200 {
		t.Fatalf("title: %d %s", rec.Code, rec.Body.String())
	}
	if got := up.efforts(); len(got) != 1 || got[0] != "low" {
		t.Fatalf("the upstream was asked at %q", got)
	}
	// Codex's auto-review, asked with no effort on a model of uni-switch's
	// whose levels Codex isn't told
	if rec := codexAsks(s, "fx/m1", "guardian", leadAsk); rec.Code != 200 {
		t.Fatalf("auto-review: %d %s", rec.Code, rec.Body.String())
	}
	if got := up.efforts(); len(got) != 2 || got[1] != "low" {
		t.Fatalf("the upstream was asked at %q", got)
	}
}

// A model whose levels uni-switch doesn't know, turning a request with no
// effort away as needing one, is asked again once at low, on Responses
// and on Chat Completions.
func TestEffortRequiredRefusalAskedAgainAtLow(t *testing.T) {
	for _, chat := range []bool{false, true} {
		fresh(t)
		up := needsEffortOn(t, "fx", chat, effortRequiredBody)
		s := New()
		if rec := codexTitleOn(s, "fx/m1"); rec.Code != 200 {
			t.Fatalf("chat %v: title: %d %s", chat, rec.Code, rec.Body.String())
		}
		if got := up.efforts(); len(got) != 2 || got[0] != "" || got[1] != "low" {
			t.Fatalf("chat %v: the upstream was asked at %q", chat, got)
		}
	}
}

// Any other 400 to a request with no effort goes back as it came, asked
// once.
func TestOtherBadRequestNotAskedAgainAtLow(t *testing.T) {
	for _, chat := range []bool{false, true} {
		fresh(t)
		up := needsEffortOn(t, "fx", chat, `{"error":{"message":"max_output_tokens is too large: 999999999","type":"invalid_request_error"}}`)
		s := New()
		if rec := codexTitleOn(s, "fx/m1"); rec.Code != 400 {
			t.Fatalf("chat %v: title: %d %s", chat, rec.Code, rec.Body.String())
		}
		if got := up.efforts(); len(got) != 1 {
			t.Fatalf("chat %v: the upstream was asked %d times, at %q", chat, len(got), got)
		}
	}
}

func TestEffortRequiredWords(t *testing.T) {
	for _, m := range []string{
		"Reasoning effort is required for this model",
		"Missing required parameter: 'reasoning.effort'.",
		"This model requires a reasoning effort",
		"reasoning_effort is mandatory",
		"Reasoning is mandatory for this endpoint and cannot be disabled.",
		"reasoning cannot be disabled for this model",
	} {
		if !effortRequired.MatchString(m) {
			t.Errorf("%q not read as an effort required", m)
		}
	}
	for _, m := range []string{
		"Unsupported parameter: 'reasoning.effort' is not supported with this model.",
		"Reasoning effort 'none' is not supported for this model. Supported values are: 'low', 'medium', 'high'.",
		"max_output_tokens is too large",
		"This model does not support reasoning",
	} {
		if effortRequired.MatchString(m) {
			t.Errorf("%q read as an effort required", m)
		}
	}
}
