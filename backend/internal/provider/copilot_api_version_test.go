package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// studentAuto stands in for Copilot's API as a Student plan sees it on
// Auto, as the owner's account showed it: POST /auto picks gpt-5.6-luna,
// which the plan may not pick by hand. A request without an API version
// has its Copilot-Session-Token left unread, so it is taken as picked by
// hand and refused; with one, the session is held to its pick ("Requested
// model not available for session" for another, or once let go).
type studentAuto struct {
	mu      sync.Mutex
	autos   int
	gone    string // a session token Copilot no longer takes
	asked   []http.Header
	answers []int
}

func (c *studentAuto) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	defer c.mu.Unlock()
	switch r.URL.Path {
	case "/models":
		io.WriteString(w, `{"data":[
		  {"id":"gpt-4.1","name":"GPT-4.1","model_picker_enabled":true,"policy":{"state":"enabled"},"supported_endpoints":["/chat/completions"],"capabilities":{"type":"chat"}},
		  {"id":"gpt-5.6-luna","name":"GPT-5.6 Luna","model_picker_enabled":true,"policy":{"state":"enabled"},"supported_endpoints":["/responses"],"capabilities":{"type":"chat"}}]}`)
		return
	case "/auto":
		c.autos++
		json.NewEncoder(w).Encode(map[string]any{
			"session_token":  "auto-" + string(rune('0'+c.autos)),
			"selected_model": map[string]any{"id": "gpt-5.6-luna", "supported_endpoints": []string{"/responses"}},
			"expires_at":     time.Now().Add(24 * time.Hour).Unix(),
		})
		return
	}
	c.asked = append(c.asked, r.Header.Clone())
	sess, model := r.Header.Get("Copilot-Session-Token"), bodyModel(b)
	code, answer := 200, `{"id":"r","model":"`+model+`","output":[]}`
	switch {
	case r.Header.Get("X-GitHub-Api-Version") == "" || sess == "":
		if model != "gpt-4.1" {
			code, answer = 400, `{"error":{"message":"The requested model is not supported.","code":"model_not_supported"}}`
		}
	case sess == c.gone || model != "gpt-5.6-luna":
		code, answer = 400, `{"error":{"message":"Requested model not available for session","code":""}}`
	}
	c.answers = append(c.answers, code)
	w.WriteHeader(code)
	io.WriteString(w, answer)
}

// #256: a Student account's Auto picked gpt-5.6-luna, and Copilot refused
// uni-switch the request ("The requested model is not supported.") while it
// served VS Code the same pick on the same account: uni-switch sent Auto's
// session token with no X-GitHub-Api-Version, which Copilot Chat sends on
// every request, and Copilot reads the session only with one. The pick is
// served now, with Copilot Chat 0.68's headers; a session Copilot no
// longer takes is asked anew.
func TestCopilotAutoPickServed(t *testing.T) {
	signIn(t)
	up := &studentAuto{}
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
	copilotPicks = map[string][]string{}
	copilotRefusedAt = map[string]map[string]time.Time{}
	copilotSeen = map[string][]string{}

	p, _ := find(All(), "copilot")
	if _, err := p.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	// the gateway's way (forward): resolved, signed, sent, and sent again
	// while the account says it is worth it
	send := func() (int, string) {
		t.Helper()
		ctx, model, err := p.ResolveAuto(context.Background(), CopilotAuto)
		if err != nil || model != "gpt-5.6-luna" {
			t.Fatalf("resolve: %q %v", model, err)
		}
		body := `{"model":"` + model + `","input":"Write a Go function that reverses a string."}`
		for range 4 {
			req, _ := http.NewRequest("POST", p.Responses+"/responses", strings.NewReader(body))
			if err := p.Sign(ctx, req, Responses, []byte(body)); err != nil {
				t.Fatal(err)
			}
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if res.StatusCode != 400 || !p.Retry(ctx, []byte(body), res.StatusCode, b) {
				return res.StatusCode, string(b)
			}
		}
		return 0, ""
	}
	if code, b := send(); code != 200 || !strings.Contains(b, "gpt-5.6-luna") {
		t.Fatalf("Auto's pick: %d %s", code, b)
	}
	h := up.asked[0]
	for k, v := range map[string]string{
		"X-GitHub-Api-Version":  "2026-01-09",
		"Copilot-Session-Token": "auto-1",
		"Editor-Version":        "vscode/1.140.0",
		"Editor-Plugin-Version": "copilot-chat/0.68.0",
		"User-Agent":            "GitHubCopilotChat/0.68.0",
	} {
		if h.Get(k) != v {
			t.Errorf("%s: %q, want %q", k, h.Get(k), v)
		}
	}
	if copilotRefuses("gho_x", "gpt-5.6-luna") {
		t.Fatal("gpt-5.6-luna noted as refused")
	}

	// a session Copilot lets go of is asked anew, and the pick served
	up.mu.Lock()
	up.gone, up.asked, up.answers = "auto-1", nil, nil
	up.mu.Unlock()
	if code, b := send(); code != 200 {
		t.Fatalf("after the session went: %d %s", code, b)
	}
	if len(up.asked) != 2 || up.asked[1].Get("Copilot-Session-Token") != "auto-2" || up.autos != 2 {
		t.Fatalf("sent with %d tries (%v), %d sessions asked; last token %q", len(up.asked), up.answers, up.autos, up.asked[len(up.asked)-1].Get("Copilot-Session-Token"))
	}
	if copilotRefuses("gho_x", "gpt-5.6-luna") {
		t.Fatal("a session let go noted as a refusal")
	}
}
