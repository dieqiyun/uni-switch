package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// Turning provider search off also skips the conversation's own search
// service. Configured APIs still search, without falling back to a plan
// when they fail; without APIs no replacement search tool is offered.
func TestSearchProviderOff(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	setHome(t, t.TempDir())
	kimi := newKimiPlan(t)
	p := provider.Provider{ID: "kimi", Name: "Kimi Code", Preset: "kimi-code-cn", Key: "sk-kimi",
		Chat: kimi.URL + "/coding/v1", Models: []string{"kimi-for-coding"}}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	st := settings.Load()
	st.Searcher = "off"
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	s := New()
	ctx := searchingOn(context.Background(), p)
	google := provider.Provider{ID: "ag", Account: &provider.Account{Agent: "antigravity", User: "u"}, Models: []string{"gemini-3.8-flash"}}
	if canSearchFor(p) || canSearchFor(google) || canSearchFor(provider.Provider{}) {
		t.Error("search tool offered without an API while provider search is off")
	}
	if _, _, err := s.webSearch(ctx, "latest go"); !errors.Is(err, errNoSearcher) {
		t.Errorf("search without an API: %v", err)
	}
	f := newSearchAPIs(t)
	if err := provider.SetSearchAPI(f.api("tavily", "tvly-k")); err != nil {
		t.Fatal(err)
	}
	if !canSearchFor(p) || !canSearchFor(provider.Provider{}) {
		t.Error("configured API does not offer a search tool")
	}
	if _, hits, err := s.webSearch(ctx, "latest go"); err != nil || len(hits) != 1 {
		t.Fatalf("API search: %v, %v", hits, err)
	}
	f.mu.Lock()
	f.broken = true
	f.mu.Unlock()
	if _, _, err := s.webSearch(ctx, "latest go"); err == nil {
		t.Error("failed API fell back to a provider")
	}
	kimi.mu.Lock()
	defer kimi.mu.Unlock()
	if len(kimi.searched) != 0 {
		t.Errorf("disabled provider searched: %v", kimi.searched)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.asked) != 2 {
		t.Errorf("API requests = %v, want two", f.asked)
	}
}

// No API means no injected search function; a relay's native search
// declaration still reaches it unchanged.
func TestSearchProviderOffForwarding(t *testing.T) {
	fresh(t)
	relay, seen := relayOfEvery(t)
	st := settings.Load()
	st.Searcher = "off"
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	for _, native := range []bool{false, true} {
		p := provider.Provider{ID: "relay", Name: "Relay", Key: "k", Models: []string{"custom-model"}, Searches: native}
		if native {
			p.Anthropic = relay.URL
		} else {
			p.Chat = relay.URL + "/v1"
		}
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
		body := `{"model":"relay/custom-model","max_tokens":100,"messages":[{"role":"user","content":"news?"}],"tools":[{"name":"Read","input_schema":{"type":"object"}},{"type":"web_search_20250305","name":"web_search"}]}`
		if code, reply := post(t, "/v1/messages", body); code != 200 {
			t.Fatalf("native %v: %d %s", native, code, reply)
		}
		paths, bodies := seen()
		if len(bodies) != 1 {
			t.Fatalf("native %v: upstream calls %v", native, paths)
		}
		if strings.Contains(bodies[0], "web_search") != native || !strings.Contains(bodies[0], "Read") {
			t.Errorf("native %v: tools changed incorrectly: %s", native, bodies[0])
		}
	}
}
