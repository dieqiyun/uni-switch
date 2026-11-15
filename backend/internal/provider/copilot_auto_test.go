package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// studentCopilot stands in for Copilot's API as a Student plan sees it: its
// /models lists models none of which it may pick by hand, and Auto's
// session picks one of them, a model served on /responses alone. It notes
// what each request asked for.
type studentCopilot struct {
	mu       sync.Mutex
	sessions int      // POST /models/session
	asked    []string // "<path> <model> <Copilot-Session-Token>"
	hints    string   // the last session request's body
	expires  int64
}

func (c *studentCopilot) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	if r.Header.Get("Authorization") != "Bearer sess" {
		w.WriteHeader(401)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case r.URL.Path == "/models":
		io.WriteString(w, `{"data":[
		  {"id":"gpt-5-mini","name":"GPT-5 mini","model_picker_enabled":true,"policy":{"state":"disabled"},"supported_endpoints":["/responses"],"capabilities":{"type":"chat"}},
		  {"id":"claude-sonnet-5","name":"Claude Sonnet 5","model_picker_category":"versatile","policy":{"state":"disabled"},"supported_endpoints":["/chat/completions","/v1/messages"],"capabilities":{"type":"chat"}}]}`)
	case r.URL.Path == "/auto":
		w.WriteHeader(404) // Auto v2 not offered: /models/session
	case r.URL.Path == "/models/session" && r.Method == "POST":
		c.sessions++
		c.hints = string(b)
		json.NewEncoder(w).Encode(map[string]any{
			"session_token":    "auto-tok-" + string(rune('0'+c.sessions)),
			"selected_model":   "gpt-5-mini",
			"available_models": []string{"gpt-5-mini", "claude-sonnet-5"},
			"expires_at":       c.expires,
			"discounted_costs": map[string]float64{"gpt-5-mini": 0.1},
		})
	default:
		c.asked = append(c.asked, r.URL.Path+" "+bodyModel(b)+" "+r.Header.Get("Copilot-Session-Token"))
		io.WriteString(w, `{"ok":true}`)
	}
}

// A Student plan, whose only choice is Auto (Discord: 请问有对copilot学生
// 套餐的支持吗？就是只能自动路由模型的那个): uni-switch lists Auto, as Copilot's
// clients do, where it used to list nothing; a request for it asks
// /models/session as the Copilot CLI does, is sent with the model Copilot
// picked and the session's token, the session kept until it nears its end.
func TestCopilotAuto(t *testing.T) {
	signIn(t)
	up := &studentCopilot{expires: time.Now().Add(time.Hour).Unix()}
	api := httptest.NewServer(up)
	defer api.Close()
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"token": "sess", "expires_at": time.Now().Add(time.Hour).Unix(), "endpoints": map[string]string{"api": api.URL}})
	}))
	defer tokens.Close()
	old := CopilotTokenURL
	CopilotTokenURL = tokens.URL
	defer func() { CopilotTokenURL = old }()
	copilotSessions = map[string]copilotSession{}
	copilotAutoSessions = map[string]copilotAutoSession{}
	copilotTerms = map[string]map[string]bool{}

	p, _ := find(All(), "copilot")
	ms, err := p.Fetch(context.Background())
	if err != nil || len(ms) != 1 || ms[0].ID != CopilotAuto || ms[0].Name != "Auto" {
		t.Fatalf("models: %+v %v", ms, err)
	}
	// the model Auto picks is served where Copilot's list says, listed or not
	if got := p.APIs("gpt-5-mini"); !slices.Equal(got, []Protocol{Responses}) {
		t.Fatalf("gpt-5-mini APIs: %v", got)
	}

	// the gateway's way: resolved first, so the API is the picked model's
	ctx, model, err := p.ResolveAuto(context.Background(), CopilotAuto)
	if err != nil || model != "gpt-5-mini" {
		t.Fatalf("resolve: %q %v", model, err)
	}
	if up.hints != `{"auto_mode":{"model_hints":["auto"]}}` {
		t.Fatalf("session asked with %s", up.hints)
	}
	send := func(ctx context.Context, body string) {
		t.Helper()
		req, _ := http.NewRequest("POST", p.Responses+"/responses", strings.NewReader(body))
		if err := p.Sign(ctx, req, Responses, []byte(body)); err != nil {
			t.Fatal(err)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
	}
	send(ctx, `{"model":"gpt-5-mini","input":"hi"}`)
	// a request that names the model itself is not Auto's
	send(context.Background(), `{"model":"gpt-5-mini","input":"hi"}`)
	// a request for "auto" signed as it is (a model test) is swapped for
	// the pick, in the same session
	send(context.Background(), `{"model":"auto","input":"hi"}`)
	want := []string{"/responses gpt-5-mini auto-tok-1", "/responses gpt-5-mini ", "/responses gpt-5-mini auto-tok-1"}
	if !slices.Equal(up.asked, want) || up.sessions != 1 {
		t.Fatalf("asked %q in %d sessions, want %q in 1", up.asked, up.sessions, want)
	}

	// a session near its end is asked for again
	copilotAutoMu.Lock()
	a := copilotAutoSessions["gho_x"]
	a.ExpiresAt = time.Now().Add(time.Minute).Unix()
	copilotAutoSessions["gho_x"] = a
	copilotAutoMu.Unlock()
	if _, _, err := p.ResolveAuto(context.Background(), CopilotAuto); err != nil || up.sessions != 2 {
		t.Fatalf("expiring session: %d sessions, %v", up.sessions, err)
	}

	// any other model is left as it was, with nothing asked
	if _, m, err := p.ResolveAuto(context.Background(), "claude-sonnet-5"); err != nil || m != "claude-sonnet-5" || up.sessions != 2 {
		t.Fatalf("other model: %q %v", m, err)
	}
}

// Auto v2 answers the picked model as an object; a refusal is said.
func TestCopilotAutoSessionShapes(t *testing.T) {
	signIn(t)
	var refuse bool
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if refuse {
			w.WriteHeader(403)
			io.WriteString(w, `{"error":{"message":"Auto mode is not available for this account"}}`)
			return
		}
		io.WriteString(w, `{"session_token":"t2","selected_model":{"id":"claude-sonnet-5","name":"Claude Sonnet 5"},"expires_at":0}`)
	}))
	defer api.Close()
	app := copilotApp{User: "octocat", Token: "gho_shapes"}
	copilotSessions = map[string]copilotSession{"gho_shapes": {Token: "sess", ExpiresAt: time.Now().Add(time.Hour).Unix(), Endpoints: struct {
		API string `json:"api"`
	}{API: api.URL}}}
	copilotAutoSessions = map[string]copilotAutoSession{}
	a, err := copilotAutoResolve(context.Background(), app, false)
	if err != nil || a.Model != "claude-sonnet-5" || a.Token != "t2" {
		t.Fatalf("v2: %+v %v", a, err)
	}
	refuse = true
	if _, err := copilotAutoResolve(context.Background(), app, true); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("refused: %v", err)
	}
}

// A student signed in from uni-switch (the editors' sign-in, #256) got "Copilot
// Auto: 404 Not Found": the session was asked without an API version, which
// VS Code's Copilot Chat sends on every CAPI request (X-GitHub-Api-Version
// 2025-10-01, @vscode/copilot-api 0.2.19). Its session answer names no
// selected_model, only available_models: the first the account knows is
// the pick.
func TestCopilotAutoSessionAsVSCode(t *testing.T) {
	signIn(t)
	var got http.Header
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			io.WriteString(w, `{"data":[{"id":"gpt-5-mini","name":"GPT-5 mini","model_picker_enabled":true,"policy":{"state":"disabled"},"supported_endpoints":["/responses"],"capabilities":{"type":"chat"}}]}`)
			return
		}
		if r.URL.Path != "/models/session" || r.Header.Get("X-GitHub-Api-Version") == "" {
			w.WriteHeader(404)
			io.WriteString(w, "Not Found")
			return
		}
		got = r.Header.Clone()
		io.WriteString(w, `{"session_token":"t3","available_models":["gpt-9-unknown","gpt-5-mini"],"expires_at":0,"discounted_costs":{"gpt-5-mini":0.1}}`)
	}))
	defer api.Close()
	app := copilotApp{User: "octocat", Token: "gho_vscode"}
	copilotSessions = map[string]copilotSession{"gho_vscode": {Token: "sess", ExpiresAt: time.Now().Add(time.Hour).Unix(), Endpoints: struct {
		API string `json:"api"`
	}{API: api.URL}}}
	copilotAutoSessions = map[string]copilotAutoSession{}
	copilotSeen = map[string][]string{}
	if _, err := copilotModels(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	a, err := copilotAutoResolve(context.Background(), app, false)
	if err != nil || a.Model != "gpt-5-mini" || a.Token != "t3" {
		t.Fatalf("session: %+v %v", a, err)
	}
	if got.Get("X-GitHub-Api-Version") != "2025-10-01" || got.Get("Copilot-Integration-Id") != "vscode-chat" || got.Get("Authorization") != "Bearer sess" {
		t.Fatalf("headers: %v", got)
	}
}

// An account Copilot has no Auto session for (404 however asked) is told
// so, and goes on with the first model it may pick by hand, when it has one.
func TestCopilotAutoUnavailable(t *testing.T) {
	signIn(t)
	var list string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			io.WriteString(w, list)
			return
		}
		w.WriteHeader(404)
		io.WriteString(w, "Not Found")
	}))
	defer api.Close()
	app := copilotApp{User: "octocat", Token: "gho_noauto"}
	copilotSessions = map[string]copilotSession{"gho_noauto": {Token: "sess", ExpiresAt: time.Now().Add(time.Hour).Unix(), Endpoints: struct {
		API string `json:"api"`
	}{API: api.URL}}}
	copilotAutoSessions = map[string]copilotAutoSession{}

	// nothing to pick by hand either: the error says Auto isn't offered
	list = `{"data":[{"id":"gpt-5-mini","name":"GPT-5 mini","model_picker_enabled":true,"policy":{"state":"disabled"},"supported_endpoints":["/responses"],"capabilities":{"type":"chat"}}]}`
	copilotModels(context.Background(), app)
	_, err := copilotAutoResolve(context.Background(), app, false)
	if err == nil || !strings.Contains(err.Error(), "doesn't offer Auto to this account") || !strings.Contains(err.Error(), "404") {
		t.Fatalf("no fallback: %v", err)
	}

	list = `{"data":[{"id":"gpt-4.1","name":"GPT-4.1","model_picker_enabled":true,"policy":{"state":"enabled"},"supported_endpoints":["/chat/completions"],"capabilities":{"type":"chat"}}]}`
	copilotModels(context.Background(), app)
	a, err := copilotAutoResolve(context.Background(), app, true)
	if err != nil || a.Model != "gpt-4.1" || a.Token != "" {
		t.Fatalf("fallback: %+v %v", a, err)
	}
}
