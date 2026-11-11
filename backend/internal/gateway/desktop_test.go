package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Claude Desktop asks for anthropic/<id>: served by the model with that id
func TestAnthropicPrefixedModel(t *testing.T) {
	f := &fake{reply: sse(
		`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"m1","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}`,
		`event: message_stop`+"\n"+`data: {"type":"message_stop"}`)}
	setup(t, provider.Anthropic, f)
	code, body := post(t, "/v1/messages", `{"model":"anthropic/fake/m1","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 || f.calls != 1 {
		t.Fatalf("anthropic/fake/m1: %d %s, %d calls", code, body, f.calls)
	}
	var sent struct{ Model string }
	if json.Unmarshal(f.got, &sent) != nil || sent.Model != "m1" {
		t.Fatalf("sent upstream as %q", f.got)
	}
	if code, body := post(t, "/v1/messages", `{"model":"anthropic/nobody/m1","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`); code != 404 {
		t.Fatalf("unknown: %d %s", code, body)
	}
	for _, id := range []string{"fake/m1", "anthropic/fake/m1"} {
		rec := httptest.NewRecorder()
		New().Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models/"+id, nil))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"fake/m1"`) {
			t.Fatalf("GET /v1/models/%s: %d %s", id, rec.Code, rec.Body)
		}
	}
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, httptest.NewRequest("HEAD", "/api/hello", nil))
	if rec.Code != 200 {
		t.Fatalf("HEAD /api/hello: %d", rec.Code)
	}
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("x-api-key", Token+"-claude-desktop")
	rec = httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"id":"`+aliasFor("fake/m1")+`"`) {
		t.Fatalf("Claude Desktop's list: %s", rec.Body)
	}
	if claudeLooking(provider.Entry{ID: "devin/claude-opus-5-5"}) != "devin/claude-opus-5-5" {
		t.Fatal("a Claude id was prefixed")
	}
	if got := unprefixed("claude-sonnet-4-5"); got != "claude-sonnet-4-5" {
		t.Fatalf("unprefixed changed %q", got)
	}
}

// Claude Desktop turns away any id naming another vendor's model, prefix
// or not (#148): deepseek, kimi, gpt, codex… get an alias it keeps, which
// uni-switch serves again, and it is known by its User-Agent without the key
func TestClaudeDesktopAliases(t *testing.T) {
	for id, want := range map[string]bool{
		"claude-sonnet-4-5": true, "anthropic/claude-x": true, "opus": true, "sonnet-4.5": true,
		"anthropic/deepseek/deepseek-flash": false, "anthropic/kimi-k2": false, "anthropic/gpt-5": false,
		"anthropic/openai/codex-mini": false, "fake/m1": false, "deepseek/claude-distill": false,
	} {
		if desktopAccepts(id) != want {
			t.Errorf("desktopAccepts(%q) = %v", id, !want)
		}
	}
	for _, id := range []string{"deepseek/deepseek-flash", "moonshot/kimi-k2.5", "openai/gpt-5.5", "codex/gpt-5-codex", "zai/glm-5"} {
		a := claudeLooking(provider.Entry{ID: id})
		if !desktopAccepts(a) || !strings.HasPrefix(a, desktopAlias) || a != claudeLooking(provider.Entry{ID: id}) {
			t.Errorf("%s listed as %s", id, a)
		}
	}

	f := &fake{reply: sse(
		`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"m1","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}`,
		`event: message_stop`+"\n"+`data: {"type":"message_stop"}`)}
	setup(t, provider.Anthropic, f)
	req := httptest.NewRequest("GET", "/v1/models?limit=1000", nil)
	req.Header.Set("x-api-key", Token)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Claude/2.9939.2 Chrome/138.0.0.0 Electron/37.0.0 Safari/537.36")
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, req)
	var list struct {
		Data []desktopRow `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	alias := aliasFor("fake/m1")
	var row *desktopRow
	for i := range list.Data {
		if list.Data[i].ID == alias {
			row = &list.Data[i]
		}
		if !desktopAccepts(list.Data[i].ID) {
			t.Errorf("Desktop would drop %q", list.Data[i].ID)
		}
	}
	if row == nil || row.DisplayName != "m1 · Fake" || row.Description != "" {
		t.Fatalf("Claude Desktop by its User-Agent: %s", rec.Body)
	}
	for _, asked := range []string{alias, alias + "[1m]"} {
		f.calls = 0
		code, body := post(t, "/v1/messages", `{"model":"`+asked+`","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
		if code != 200 || f.calls != 1 {
			t.Fatalf("%s: %d %s", asked, code, body)
		}
	}
	rec = httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models/"+alias, nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"fake/m1"`) {
		t.Fatalf("GET /v1/models/%s: %d %s", alias, rec.Code, rec.Body)
	}
	if _, ok := aliased(desktopAlias + "0000000000"); ok {
		t.Fatal("an alias no model has resolved")
	}
}

// Claude Desktop set up by uni-switch sends the key uni-switch gave it, from its
// own client and from the Claude Code its Code tab runs alike; without it,
// its Electron User-Agent still says who it is.
func TestClaudeDesktopKnown(t *testing.T) {
	for _, c := range []struct{ key, ua string }{
		{TokenFor("claude-desktop"), "Mozilla/5.0 (Macintosh) Claude/2.7032.0 Chrome/138.0 Electron/37.0 Safari/537.36"},
		{TokenFor("claude-desktop"), "claude-cli/2.1.200 (external, claude-desktop)"},
		{"", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Claude/2.7032.0 Chrome/138.0 Electron/37.0 Safari/537.36"},
	} {
		r := httptest.NewRequest("GET", "/v1/models", nil)
		r.Header.Set("User-Agent", c.ua)
		if c.key != "" {
			r.Header.Set("Authorization", "Bearer "+c.key)
		}
		if got := agentOf(r); got != "claude-desktop" {
			t.Errorf("%q %q: %s", c.key, c.ua, got)
		}
	}
}

type desktopRow struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
}

// DesktopNumber is the number Claude Desktop's menu says beside a model:
// only one it names after its own catalog, a Claude model with reasoning
// levels (uni-switch-<n>.anthropic.<model>), so the Providers page says it
// there alone (#2578). A row Desktop names by display_name, which says the
// provider, or by the model's own id, gets none.
func TestDesktopNumber(t *testing.T) {
	opus := provider.Entry{ID: "a/claude-opus-5-5", Model: "claude-opus-5-5", Efforts: []string{"low"}}
	n := DesktopNumber(opus)
	if n != aliasNumber(opus.ID) || claudeLooking(opus) != "uni-switch-"+n+".anthropic.claude-opus-5-5" {
		t.Errorf("an effort Claude model: DesktopNumber = %q, listed as %s", n, claudeLooking(opus))
	}
	for _, e := range []provider.Entry{
		// mythos-uni-switch-<n>, named by display_name
		{ID: "a/glm-5.3", Model: "glm-5.3", Efforts: []string{"low"}},
		// anthropic/uni-switch-<n>, named by display_name
		{ID: "a/glm-5.3", Model: "glm-5.3"},
		// its own id, which carries no number
		{ID: "a/claude-opus-5-5", Model: "claude-opus-5-5"},
		// a group led by a Claude model keeps its own name (mythos-uni-switch-<n>)
		{ID: "group/opus", Model: "claude-opus-5-5", Group: "opus", Efforts: []string{"low"}},
	} {
		if got := DesktopNumber(e); got != "" {
			t.Errorf("%s (listed as %s): DesktopNumber = %q, want none", e.ID, claudeLooking(e), got)
		}
	}
}
