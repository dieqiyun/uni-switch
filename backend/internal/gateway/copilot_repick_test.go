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

// copilotFake signs a Copilot account in (its GitHub token gh) to a fake
// Copilot API served by api.
func copilotFake(t *testing.T, gh string, api http.HandlerFunc) {
	t.Helper()
	fresh(t)
	cfg := os.Getenv("XDG_CONFIG_HOME")
	os.MkdirAll(filepath.Join(cfg, "github-copilot"), 0o755)
	os.WriteFile(filepath.Join(cfg, "github-copilot", "apps.json"), mustJSON(map[string]any{
		"github.com:Iv1.x": map[string]any{"user": "student", "oauth_token": gh},
	}), 0o600)
	up := httptest.NewServer(api)
	t.Cleanup(up.Close)
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			json.NewEncoder(w).Encode(map[string]any{"token": "sess", "expires_at": time.Now().Add(time.Hour).Unix(), "endpoints": map[string]string{"api": up.URL}})
			return
		}
		io.WriteString(w, `{"copilot_plan":"individual","access_type_sku":"free_educational_quota"}`)
	}))
	t.Cleanup(hub.Close)
	oldTok, oldUser := provider.CopilotTokenURL, provider.CopilotUserURL
	provider.CopilotTokenURL, provider.CopilotUserURL = hub.URL+"/token", hub.URL+"/user"
	t.Cleanup(func() { provider.CopilotTokenURL, provider.CopilotUserURL = oldTok, oldUser })
}

const copilotStudentModels = `{"data":[
  {"id":"gpt-5.3-codex","name":"GPT-5.3-Codex","model_picker_enabled":true,"policy":{"state":"enabled"},"supported_endpoints":["/responses"],"capabilities":{"type":"chat"}},
  {"id":"gpt-4.1","name":"GPT-4.1","model_picker_enabled":true,"is_chat_default":true,"policy":{"state":"enabled"},"capabilities":{"type":"chat"}}]}`

const copilotNotSupported = `{"error":{"message":"The requested model is not supported.","code":"model_not_supported","param":"model","type":"invalid_request_error"}}`

// #256 (infinitr0us, a real Student account): Auto picked gpt-5.3-codex,
// served on /responses alone, which Copilot refused the account; Auto then
// picked gpt-4.1, served on chat completions alone, and the request was
// sent again on /responses, to be refused "model gpt-4.1 is not supported
// via Responses API". The new pick is asked for on its own API.
func TestCopilotAutoRepickTakesItsAPI(t *testing.T) {
	var mu sync.Mutex
	var asked []string // "<path> <model>"
	copilotFake(t, "gho_student256repick", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/models":
			io.WriteString(w, copilotStudentModels)
		case "/auto":
			w.WriteHeader(404) // Auto v2 not offered: /models/session
		case "/models/session":
			io.WriteString(w, `{"session_token":"auto-tok","selected_model":"gpt-5.3-codex","available_models":["gpt-5.3-codex","gpt-4.1"],"expires_at":`+
				mustString(time.Now().Add(time.Hour).Unix())+`}`)
		default:
			m := modelOf(b)
			asked = append(asked, r.URL.Path+" "+m)
			switch {
			case m == "gpt-5.3-codex":
				w.WriteHeader(400)
				io.WriteString(w, copilotNotSupported)
			case r.URL.Path != "/chat/completions":
				w.WriteHeader(400)
				io.WriteString(w, `{"error":{"message":"model `+m+` is not supported via Responses API.","code":"unsupported_api_for_model"}}`)
			default:
				io.WriteString(w, `{"id":"c1","model":"gpt-4.1","choices":[{"index":0,"message":{"role":"assistant","content":"hello from gpt-4.1"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":3}}`)
			}
		}
	})
	s := New()
	code, body := postAs(t, s, "", `{"model":"copilot/auto","messages":[{"role":"user","content":"Reply with the single word: ok"}]}`)
	if code != 200 || !strings.Contains(body, "hello from gpt-4.1") {
		t.Fatalf("copilot/auto: %d %s", code, body)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(asked, "|") != "/responses gpt-5.3-codex|/chat/completions gpt-4.1" {
		t.Fatalf("Copilot was asked %q", asked)
	}
}

// Copilot Chat 0.69 asks POST /auto, at API version 2026-08-01, with the
// turn's prompt: the model it picks, on the APIs it names, signed with its
// session; /models/session isn't asked.
func TestCopilotAutoAsksAuto(t *testing.T) {
	var mu sync.Mutex
	var asked []string
	var auto struct {
		version, prompt string
	}
	copilotFake(t, "gho_student256v2", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/models":
			io.WriteString(w, copilotStudentModels)
		case "/auto":
			var q struct {
				Prompt string `json:"prompt"`
			}
			json.Unmarshal(b, &q)
			auto.version, auto.prompt = r.Header.Get("X-GitHub-Api-Version"), q.Prompt
			io.WriteString(w, `{"session_token":"v2-tok","selected_model":{"id":"gpt-4.1-mini","supported_endpoints":["/chat/completions"],"capabilities":{"type":"chat"}},"expires_at":`+
				mustString(time.Now().Add(24*time.Hour).Unix())+`}`)
		case "/models/session":
			asked = append(asked, "/models/session")
			w.WriteHeader(500)
		default:
			asked = append(asked, r.URL.Path+" "+modelOf(b)+" "+r.Header.Get("Copilot-Session-Token"))
			io.WriteString(w, `{"id":"c1","model":"gpt-4.1-mini","choices":[{"index":0,"message":{"role":"assistant","content":"hello from auto"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":3}}`)
		}
	})
	s := New()
	code, body := postAs(t, s, "", `{"model":"copilot/auto","messages":[{"role":"system","content":"be brief"},{"role":"user","content":"Refactor this function"}]}`)
	if code != 200 || !strings.Contains(body, "hello from auto") {
		t.Fatalf("copilot/auto: %d %s", code, body)
	}
	mu.Lock()
	defer mu.Unlock()
	if auto.version != "2026-08-01" || auto.prompt != "Refactor this function" {
		t.Errorf("/auto was asked at %q with %q", auto.version, auto.prompt)
	}
	if strings.Join(asked, "|") != "/chat/completions gpt-4.1-mini v2-tok" {
		t.Fatalf("Copilot was asked %q", asked)
	}
}

// #256 again (infinitr0us, v0.1.916, a Student account): the first tries
// of copilot/auto were refused, then gpt-4.1 answered, and the route
// logged each refused try as "auto": which model was refused, and whether
// /auto or /models/session picked it, wasn't told. Each try now names the
// models Auto picked for it, where from and Copilot's refusal; /auto is
// asked at Copilot Chat's tier, and once it has answered with a model the
// account was refused, the model it may pick by hand stands in, not
// /models/session's, which names the same few for every plan.
func TestCopilotAutoPicksInRoute(t *testing.T) {
	var mu sync.Mutex
	var asked, tiers []string
	copilotFake(t, "gho_student256picks", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/models":
			io.WriteString(w, copilotStudentModels)
		case "/auto":
			var q struct {
				Tier string `json:"tier"`
			}
			json.Unmarshal(b, &q)
			tiers = append(tiers, q.Tier)
			io.WriteString(w, `{"session_token":"v2-tok","selected_model":{"id":"gpt-5.3-codex","supported_endpoints":["/responses"]},"expires_at":`+
				mustString(time.Now().Add(24*time.Hour).Unix())+`}`)
		case "/models/session":
			asked = append(asked, "/models/session")
			io.WriteString(w, `{"session_token":"s-tok","selected_model":"gpt-5.4-mini","available_models":["gpt-5.4-mini"],"expires_at":`+
				mustString(time.Now().Add(time.Hour).Unix())+`}`)
		default:
			m := modelOf(b)
			asked = append(asked, r.URL.Path+" "+m)
			if m != "gpt-4.1" {
				w.WriteHeader(400)
				io.WriteString(w, copilotNotSupported)
				return
			}
			io.WriteString(w, `{"id":"c1","model":"gpt-4.1","choices":[{"index":0,"message":{"role":"assistant","content":"hello from gpt-4.1"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":3}}`)
		}
	})
	s := New()
	code, body := postAs(t, s, "", `{"model":"copilot/auto","messages":[{"role":"user","content":"Reply with the single word: ok"}]}`)
	if code != 200 || !strings.Contains(body, "hello from gpt-4.1") {
		t.Fatalf("copilot/auto: %d %s", code, body)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(asked, "|") != "/responses gpt-5.3-codex|/chat/completions gpt-4.1" {
		t.Errorf("Copilot was asked %q", asked)
	}
	if len(tiers) == 0 || tiers[0] != "balance" {
		t.Errorf("/auto was asked at the tiers %q", tiers)
	}
	var picks []string
	for _, tr := range lastRoute(s).Tries {
		for _, p := range tr.Auto {
			picks = append(picks, p.Model+" "+p.Via+" "+map[bool]string{true: "refused", false: "ok"}[p.Refused != ""])
		}
	}
	if strings.Join(picks, "|") != "gpt-5.3-codex /auto refused|gpt-4.1 fallback ok" {
		t.Fatalf("the route told the picks %q", picks)
	}
	tries := lastRoute(s).Tries
	last := tries[len(tries)-1].Auto
	if !strings.Contains(tries[0].Auto[0].Refused, "requested model is not supported") || !strings.Contains(last[len(last)-1].Skipped, "picked gpt-5.3-codex, which the account was refused") {
		t.Fatalf("the picks said %+v", tries)
	}
}

// copilotStudentList is a Student plan's /models as Copilot lists it now
// (#256, 22:07): nothing flagged as the chat default or fallback, nothing
// billed, gpt-4.1 last and out of the picker; the plan is served gpt-4.1
// alone.
const copilotStudentList = `{"data":[
  {"id":"gpt-5.4-mini","model_picker_enabled":true,"policy":{"state":"enabled"},"supported_endpoints":["/chat/completions","/responses"],"capabilities":{"type":"chat"}},
  {"id":"gpt-6-luna","model_picker_enabled":true,"policy":{"state":"enabled"},"supported_endpoints":["/responses"],"capabilities":{"type":"chat"}},
  {"id":"kimi-k3","model_picker_enabled":true,"policy":{"state":"enabled"},"supported_endpoints":["/chat/completions"],"capabilities":{"type":"chat"}},
  {"id":"mai-code-1.1-flash","model_picker_enabled":true,"policy":{"state":"enabled"},"supported_endpoints":["/chat/completions"],"capabilities":{"type":"chat"}},
  {"id":"gpt-5-mini","model_picker_enabled":true,"policy":{"state":"enabled"},"supported_endpoints":["/chat/completions","/responses"],"capabilities":{"type":"chat"}},
  {"id":"claude-haiku-4.5","model_picker_enabled":true,"policy":{"state":"enabled"},"supported_endpoints":["/chat/completions"],"capabilities":{"type":"chat"}},
  {"id":"gpt-4.1","model_picker_enabled":false,"model_picker_category":"versatile","policy":{"state":"enabled"},"capabilities":{"type":"chat"}}]}`

// #256 (infinitr0us, 22:07, v0.1.921): /auto picked gpt-5.6-luna, which
// Copilot refused "The requested model is not supported."; uni-switch then went
// down the list in its order, gpt-4.1 last, five more refusals, and the
// request failed with 400 before reaching gpt-4.1. The reply named
// mai-code-1.1-flash, a refused pick. gpt-4.1 is now the first fallback,
// the reply names it, and the route says each pick's API and whether Auto's
// session token went with it.
func TestCopilotAutoFallsBackToBase(t *testing.T) {
	var mu sync.Mutex
	var asked []string
	copilotFake(t, "gho_student256base", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/models":
			io.WriteString(w, copilotStudentList)
		case "/auto":
			io.WriteString(w, `{"session_token":"v2-tok","selected_model":{"id":"gpt-5.6-luna","supported_endpoints":["/responses"]},"expires_at":`+
				mustString(time.Now().Add(24*time.Hour).Unix())+`}`)
		case "/models/session":
			http.NotFound(w, r)
		default:
			m := modelOf(b)
			asked = append(asked, r.URL.Path+" "+m)
			if m != "gpt-4.1" {
				w.WriteHeader(400)
				io.WriteString(w, copilotNotSupported)
				return
			}
			io.WriteString(w, `{"id":"c1","model":"gpt-4.1","choices":[{"index":0,"message":{"role":"assistant","content":"hello from gpt-4.1"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":3}}`)
		}
	})
	s := New()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"model":"copilot/auto","messages":[{"role":"user","content":"Reply with the single word: ok"}]}`)))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "hello from gpt-4.1") {
		t.Fatalf("copilot/auto: %d %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Uni-Switch-Model"); got != "copilot/gpt-4.1" {
		t.Errorf("X-Uni-Switch-Model %q, want copilot/gpt-4.1", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(asked, "|") != "/responses gpt-5.6-luna|/chat/completions gpt-4.1" {
		t.Errorf("Copilot was asked %q", asked)
	}
	var picks []provider.AutoPick
	for _, tr := range lastRoute(s).Tries {
		picks = append(picks, tr.Auto...)
	}
	if len(picks) != 2 || picks[0].Model != "gpt-5.6-luna" || picks[0].Via != "/auto" || picks[0].API != "/responses" || !picks[0].Session ||
		picks[1].Model != "gpt-4.1" || picks[1].API != "/chat/completions" || picks[1].Session {
		t.Fatalf("the route told the picks %+v", picks)
	}
}
