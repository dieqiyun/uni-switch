package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func TestClientProviderSelectionKeepsCodexNativeCallsOffChatGPT(t *testing.T) {
	var official int
	chatgpt(t, func(w http.ResponseWriter, r *http.Request) { official++; w.WriteHeader(500) })
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(`data: {"type":"response.completed","response":{"id":"selected-provider","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`))
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "chosen", Name: "Chosen", Key: "synthetic", Responses: up.URL + "/v1", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetAgentProvider("codex", "chosen"); err != nil {
		t.Fatal(err)
	}
	s := New()
	for _, model := range []string{"m1", "unknown-native-model"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(`{"model":"`+model+`","stream":true,"input":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Authorization", "Bearer chatgpt-synthetic")
		req.Header.Set("User-Agent", "codex_cli_rs/0.100.0")
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "selected-provider") {
			t.Fatalf("bare model did not use selection: %d %s", rec.Code, rec.Body)
		}
		if official != 0 {
			t.Fatal("native request spent ChatGPT instead of the selected provider")
		}
	}
	// A side-call helper must not bypass the selection by relaying directly.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", CodexPath+"/responses", nil)
	req.Header.Set("Authorization", "Bearer uni-switch-codex")
	s.codexUpstreamOn(rec, req, "/responses", []byte(`{"model":"gpt-5.5"}`), nil)
	if rec.Code != 403 || official != 0 {
		t.Fatal("direct native relay bypassed the selected provider")
	}
}

func TestClientProviderSelectionBlocksOtherProviderAndFallback(t *testing.T) {
	plan := &fake{t: t, ctype: "application/json", code: 402, reply: `{"error":{"message":"Insufficient Balance"}}`}
	spare := &fake{t: t, ctype: "application/json", reply: `{"id":"from-spare","choices":[]}`}
	twoProviders(t, plan, spare)
	if err := provider.SetAgentProvider("codex", "plan"); err != nil {
		t.Fatal(err)
	}
	s := New()
	for _, model := range []string{"plan/m1", "m1", "spare/m2"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"`+model+`","messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Authorization", "Bearer uni-switch-codex")
		s.Handler().ServeHTTP(rec, req)
		if rec.Code < 400 {
			t.Fatalf("%s: %d %s", model, rec.Code, rec.Body)
		}
		if spare.calls != 0 {
			t.Fatalf("%s spent another provider", model)
		}
		if model == "spare/m2" && rec.Code != 403 {
			t.Fatalf("expected explicit provider refusal: %d %s", rec.Code, rec.Body)
		}
	}
}
