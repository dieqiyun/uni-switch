package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A Copilot Student plan may pick only Auto (Discord: 请问有对copilot学生
// 套餐的支持吗？就是只能自动路由模型的那个). A chat request for copilot/auto
// is sent as Copilot's clients send it: the model Copilot's /models/session
// picked, on the API that model is served on (here /responses alone, so the
// Chat request is translated), with the session's Copilot-Session-Token.
func TestCopilotAutoThroughGateway(t *testing.T) {
	fresh(t)
	cfg := os.Getenv("XDG_CONFIG_HOME")
	os.MkdirAll(filepath.Join(cfg, "github-copilot"), 0o755)
	os.WriteFile(filepath.Join(cfg, "github-copilot", "apps.json"), mustJSON(map[string]any{
		"github.com:Iv1.x": map[string]any{"user": "student", "oauth_token": "gho_student"},
	}), 0o600)

	var mu sync.Mutex
	var sessions int
	var asked []string // "<path> <model> <session token>"
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/models":
			io.WriteString(w, `{"data":[
			  {"id":"gpt-5-mini","name":"GPT-5 mini","model_picker_enabled":true,"policy":{"state":"disabled"},"supported_endpoints":["/responses"],"capabilities":{"type":"chat"}}]}`)
		case "/auto":
			w.WriteHeader(404) // Auto v2 not offered: /models/session
		case "/models/session":
			sessions++
			io.WriteString(w, `{"session_token":"auto-tok","selected_model":"gpt-5-mini","available_models":["gpt-5-mini"],"expires_at":`+
				mustString(time.Now().Add(time.Hour).Unix())+`}`)
		default:
			asked = append(asked, r.URL.Path+" "+modelOf(b)+" "+r.Header.Get("Copilot-Session-Token"))
			if r.URL.Path != "/responses" {
				w.WriteHeader(400)
				io.WriteString(w, `{"error":{"message":"model gpt-5-mini is not supported via /chat/completions"}}`)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, sse(
				`data: {"type":"response.created","response":{"id":"r1","model":"gpt-5-mini"}}`,
				`data: {"type":"response.output_text.delta","delta":"hello from auto"}`,
				`data: {"type":"response.completed","response":{"id":"r1","model":"gpt-5-mini","usage":{"input_tokens":3,"output_tokens":3}}}`))
		}
	}))
	defer api.Close()
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			json.NewEncoder(w).Encode(map[string]any{"token": "sess", "expires_at": time.Now().Add(time.Hour).Unix(), "endpoints": map[string]string{"api": api.URL}})
			return
		}
		io.WriteString(w, `{"copilot_plan":"individual","access_type_sku":"free_educational_quota"}`)
	}))
	defer gh.Close()
	oldTok, oldUser := provider.CopilotTokenURL, provider.CopilotUserURL
	provider.CopilotTokenURL, provider.CopilotUserURL = gh.URL+"/token", gh.URL+"/user"
	defer func() { provider.CopilotTokenURL, provider.CopilotUserURL = oldTok, oldUser }()

	s := New()
	for range 2 {
		code, body := postAs(t, s, "", `{"model":"copilot/auto","messages":[{"role":"user","content":"hi"}]}`)
		if code != 200 || !strings.Contains(body, "hello from auto") {
			t.Fatalf("copilot/auto: %d %s", code, body)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(asked, "|") != "/responses gpt-5-mini auto-tok|/responses gpt-5-mini auto-tok" || sessions != 1 {
		t.Fatalf("Copilot was asked %q in %d sessions", asked, sessions)
	}
	c := s.Recent()[0]
	if c.Provider != "copilot" || c.Model != "copilot/auto" && c.Model != "auto" || c.Usage.Served != "gpt-5-mini" {
		t.Fatalf("call: %+v", c)
	}
}

func mustString(n int64) string { b, _ := json.Marshal(n); return string(b) }
