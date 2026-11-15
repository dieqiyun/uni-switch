package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A provider of the user's own, at its address on the API it speaks, is
// added from the TUI as the app's custom provider editor and uni-switch provider
// add <name> url=… add it (hezz1891 on Discord: on a server, the TUI had
// vendors uni-switch knows alone).
func TestTUIAddsACustomProvider(t *testing.T) {
	home(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer sk-relay" {
			http.Error(w, `{"error":"no"}`, http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []map[string]any{
			{"id": "qwen3.6-max", "object": "model"}, {"id": "glm-5.3", "object": "model"},
		}})
	}))
	defer srv.Close()

	m := press(t, model{w: 140, h: 40}, "2", "a")
	if !strings.Contains(m.View(), "or custom for your own API") {
		t.Fatalf("the vendors' line doesn't say how to add one's own:\n%s", m.View())
	}
	m = typeIn(m, "custom")
	for _, id := range []string{"custom-openai", "custom-responses", "custom-anthropic", "custom-gemini"} {
		if !strings.Contains(m.View(), id) {
			t.Fatalf("custom doesn't offer %s:\n%s", id, m.View())
		}
	}
	m = typeIn(m, "-openai")
	m = press(t, m, "enter")
	if m.mode != modeAsk || m.ask.input.Placeholder != "https://api.example.com/v1" {
		t.Fatalf("custom asked %q (mode %v), not the base URL", m.ask.input.Placeholder, m.mode)
	}
	// not an address: said so, nothing asked after it
	m = typeIn(m, "api.example.com")
	m = press(t, m, "enter")
	wantFlash(t, m, false, "http:// or https://")

	m = press(t, m, "a")
	m = typeIn(m, "custom-openai")
	m = press(t, m, "enter")
	m = typeIn(m, srv.URL+"/v1/")
	m = press(t, m, "enter")
	if m.mode != modeAsk || m.ask.input.Placeholder != "127.0.0.1" {
		t.Fatalf("after the URL it asked %q (mode %v), not the name", m.ask.input.Placeholder, m.mode)
	}
	m = typeIn(m, "My Relay")
	m = press(t, m, "enter")
	if m.mode != modeAsk || m.ask.input.Placeholder != "the API key" {
		t.Fatalf("after the name it asked %q (mode %v), not the key", m.ask.input.Placeholder, m.mode)
	}
	m = typeIn(m, "sk-relay")
	m = press(t, m, "enter")
	wantFlash(t, m, true, "added My Relay · 2 models")
	p, err := provider.Find("my-relay")
	if err != nil {
		t.Fatal(err)
	}
	if p.Chat != srv.URL+"/v1" || p.Responses != "" || p.Anthropic != "" || p.Key != "sk-relay" || p.Preset != "" {
		t.Fatalf("saved chat %q responses %q anthropic %q key %q preset %q", p.Chat, p.Responses, p.Anthropic, p.Key, p.Preset)
	}

	// each API sets its own URL; a name left empty is the host's. (Each
	// has a key of its own: the same host with the same key is the
	// provider already there, which provider.Add turns away. Nothing
	// listens at :1, so the list is refused at once.)
	for _, c := range []struct {
		id, name, want, url string
		got                 func(provider.Provider) string
	}{
		{"custom-responses", "", "127.0.0.1", "http://127.0.0.1:1/v1", func(p provider.Provider) string { return p.Responses }},
		{"custom-anthropic", "Claude relay", "Claude relay", "http://127.0.0.1:1", func(p provider.Provider) string { return p.Anthropic }},
		{"custom-gemini", "Gemini relay", "Gemini relay", "http://127.0.0.1:1/v1beta", func(p provider.Provider) string { return p.Gemini }},
	} {
		m = press(t, m, "a")
		m = typeIn(m, c.id)
		m = press(t, m, "enter")
		m = typeIn(m, c.url)
		m = press(t, m, "enter")
		if c.name != "" {
			m = typeIn(m, c.name)
		}
		m = press(t, m, "enter")
		m = typeIn(m, "sk-"+c.id)
		m = press(t, m, "enter")
		i := slices.IndexFunc(provider.All(), func(q provider.Provider) bool { return q.Name == c.want })
		if i < 0 {
			t.Errorf("%s: no provider named %s (flash %q)", c.id, c.want, m.flash)
			continue
		}
		q := provider.All()[i]
		if c.got(q) != c.url || q.Chat != "" {
			t.Errorf("%s: saved %q (chat %q), not %q", c.id, c.got(q), q.Chat, c.url)
		}
	}
}

// w on a provider of the user's own asks each base URL it has, the one
// now typed in, as uni-switch provider set <id> url=… changes it
// (luluxiu_1_4392 on Discord: on Linux, the TUI had no way to): enter
// alone keeps one, a URL that isn't one is refused, and the others stay.
func TestTUIEditsACustomProvidersBaseURL(t *testing.T) {
	home(t)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "sk-r", Models: []string{"m"},
		Chat: "https://old.example.com/v1", Anthropic: "https://old.example.com"}); err != nil {
		t.Fatal(err)
	}
	m := press(t, model{w: 140, h: 40}, "2")
	at := func(m model, id string) model {
		t.Helper()
		m.reloadProviders()
		i := slices.IndexFunc(m.provs, func(p provider.Provider) bool { return p.ID == id })
		if i < 0 {
			t.Fatalf("no provider %s", id)
		}
		m.prow = i
		return m
	}

	// one base URL: asked, typed in as it is now, and changed
	m = at(m, "a")
	m = press(t, m, "w")
	if m.mode != modeAsk || m.ask.input.Value() != "http://127.0.0.1:1/v1" {
		t.Fatalf("w opened %q (mode %v), not the base URL now", m.ask.input.Value(), m.mode)
	}
	m = press(t, m, "ctrl+u")
	m = typeIn(m, "https://new.example.com/v1/")
	m = press(t, m, "enter")
	wantFlash(t, m, true, "A base URL https://new.example.com/v1")
	if p, _ := provider.Find("a"); p.Chat != "https://new.example.com/v1" || p.Key != "ka" || len(p.Models) != 2 {
		t.Fatalf("a after w: %+v", p)
	}

	// not an address: refused, nothing saved
	m = press(t, m, "w", "ctrl+u")
	m = typeIn(m, "new.example.com")
	m = press(t, m, "enter")
	wantFlash(t, m, false, "http:// or https://")
	if p, _ := provider.Find("a"); p.Chat != "https://new.example.com/v1" {
		t.Fatalf("a refused URL left %q", p.Chat)
	}

	// two: each asked in turn, the one kept as it was left alone
	m = at(m, "relay")
	m = press(t, m, "w")
	if m.ask.input.Value() != "https://old.example.com/v1" || !strings.Contains(m.ask.hint, "Chat Completions") {
		t.Fatalf("first asked %q (%s)", m.ask.input.Value(), m.ask.hint)
	}
	m = press(t, m, "enter")
	if m.mode != modeAsk || m.ask.input.Value() != "https://old.example.com" || !strings.Contains(m.ask.hint, "Anthropic") {
		t.Fatalf("second asked %q (mode %v, %s)", m.ask.input.Value(), m.mode, m.ask.hint)
	}
	m = press(t, m, "ctrl+u")
	m = typeIn(m, "https://claude.example.com")
	m = press(t, m, "enter")
	wantFlash(t, m, true, "Relay base URL https://claude.example.com")
	if p, _ := provider.Find("relay"); p.Chat != "https://old.example.com/v1" || p.Anthropic != "https://claude.example.com" || p.Responses != "" {
		t.Fatalf("relay after w: chat %q anthropic %q responses %q", p.Chat, p.Anthropic, p.Responses)
	}
	// both left as they are
	m = press(t, m, "w", "enter", "enter")
	wantFlash(t, m, true, "Relay base URL unchanged")
}
