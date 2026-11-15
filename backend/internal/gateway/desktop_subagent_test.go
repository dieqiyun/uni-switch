package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// desktopElectronUA is Desktop's own client (its titles, isClaudeDesktop);
// desktopCodeUA the Claude Code of its Code tab, made of the version and
// entrypoint Desktop runs it with (2.1.295, CLAUDE_CODE_ENTRYPOINT
// claude-desktop-3p) in Claude Code's User-Agent form.
const desktopElectronUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Claude/2.7032.0 Chrome/138.0.7204.251 Electron/37.2.3 Safari/537.36"

const desktopCodeUA = "claude-cli/2.1.295 (external, claude-desktop-3p, agent-sdk/0.3.295)"

// desktopSubagentUpstream records only message and count requests. Its
// lock also covers reads, since a fake's handlers run on other goroutines.
type desktopSubagentUpstream struct {
	mu          sync.Mutex
	model, path string
}

func (u *desktopSubagentUpstream) seen() (string, string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.model, u.path
}

// desktopSubagentSetup is a provider serving m1, m2 and m3 on a fake
// Anthropic upstream that notes the model and path of the last request.
func desktopSubagentSetup(t *testing.T) *desktopSubagentUpstream {
	t.Helper()
	fresh(t)
	noRests()
	t.Cleanup(noRests)
	u := &desktopSubagentUpstream{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" && r.URL.Path != "/v1/messages/count_tokens" {
			w.WriteHeader(404)
			return
		}
		body, _ := io.ReadAll(r.Body)
		model := modelOf(body)
		u.mu.Lock()
		u.model, u.path = model, r.URL.Path
		u.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/count_tokens") {
			io.WriteString(w, `{"input_tokens":12}`)
			return
		}
		io.WriteString(w, `{"id":"reply","type":"message","role":"assistant","model":"`+model+`","content":[{"type":"text","text":"on it"}],"stop_reason":"end_turn","usage":{"input_tokens":7,"output_tokens":1}}`)
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Key: "k", Anthropic: up.URL, Models: []string{"m1", "m2", "m3"}}); err != nil {
		t.Fatal(err)
	}
	return u
}

func desktopSubagentSet(t *testing.T, id string) {
	t.Helper()
	if err := provider.SetDesktopSubagentModel(id); err != nil {
		t.Fatal(err)
	}
}

// desktopSubagentAsk sends a request shaped like one of Desktop's Claude
// Code: its key, User-Agent and session, a subagent's agent id when sub is
// set, and Claude Code's tools when tools is.
func desktopSubagentAsk(t *testing.T, s *Server, key, model, sub, path string, tools bool, uas ...string) {
	t.Helper()
	body := map[string]any{"model": model, "max_tokens": 200, "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	if tools {
		body["tools"] = []any{map[string]any{"name": "Bash", "input_schema": map[string]any{"type": "object"}}}
	}
	b, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", path, strings.NewReader(string(b)))
	r.Header.Set("x-api-key", key)
	ua := desktopCodeUA
	if len(uas) != 0 {
		ua = uas[0]
	}
	r.Header.Set("User-Agent", ua)
	r.Header.Set("x-claude-code-session-id", "lead-session")
	if sub != "" {
		r.Header.Set("x-claude-code-agent-id", sub)
		r.Header.Set("x-claude-code-parent-agent-id", "parent-agent")
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("%s: %d %s", model, w.Code, w.Body.String())
	}
}

// A Desktop subagent's turn goes on the model set for Desktop's subagents,
// whether it named the model by its catalog id or by the alias Desktop was
// listed; the trace and the ledger keep the model it asked for.
func TestDesktopSubagentMoved(t *testing.T) {
	for _, alias := range []bool{false, true} {
		t.Run(map[bool]string{false: "catalog", true: "alias"}[alias], func(t *testing.T) {
			u := desktopSubagentSetup(t)
			desktopSubagentSet(t, "fake/m2")
			s := New()
			model := "fake/m1"
			if alias {
				model = aliasFor(model)
			}
			desktopSubagentAsk(t, s, Token+"-claude-desktop", model, "worker", "/v1/messages", true)
			if got, _ := u.seen(); got != "m2" {
				t.Fatalf("subagent sent %q, want m2", got)
			}
			r := lastRoute(s)
			if r.Subagent == nil || *r.Subagent != (SubagentPick{Asked: "fake/m1", To: "fake/m2"}) || r.Model != "fake/m1" || r.Kind != "" {
				t.Fatalf("trace: model %q kind %q subagent %+v", r.Model, r.Kind, r.Subagent)
			}
			log, err := os.ReadFile(usage.Path())
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(log)), "\n")
			var rec usage.Record
			if err := json.Unmarshal([]byte(lines[len(lines)-1]), &rec); err != nil {
				t.Fatal(err)
			}
			if rec.Requested != "fake/m1" || rec.Model != "m2" || rec.Subagent != "worker" || rec.ParentAgent != "parent-agent" {
				t.Fatalf("ledger: requested %q model %q subagent %q parent %q", rec.Requested, rec.Model, rec.Subagent, rec.ParentAgent)
			}
		})
	}
}

// The lead's turns, a subagent's tool-less helper calls, terminal Claude
// Code, Codex and other agents, and every request with no model set or the
// model set asked for, go on as asked, the trace saying nothing of it.
func TestDesktopSubagentLeavesOtherRequests(t *testing.T) {
	for _, tc := range []struct {
		name, key, sub, target string
		tools                  bool
	}{
		{"lead", "claude-desktop", "", "fake/m2", true},
		{"helper", "claude-desktop", "worker", "fake/m2", false},
		{"terminal", "claude", "worker", "fake/m2", true},
		{"codex", "codex", "worker", "fake/m2", true},
		{"other", "curl", "worker", "fake/m2", true},
		{"unset", "claude-desktop", "worker", "", true},
		{"same", "claude-desktop", "worker", "fake/m1", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := desktopSubagentSetup(t)
			desktopSubagentSet(t, tc.target)
			s := New()
			desktopSubagentAsk(t, s, Token+"-"+tc.key, "fake/m1", tc.sub, "/v1/messages", tc.tools)
			if got, _ := u.seen(); got != "m1" {
				t.Fatalf("sent %q, want m1", got)
			}
			if r := lastRoute(s); r.Subagent != nil {
				t.Fatalf("untouched request traced %+v", r.Subagent)
			}
		})
	}
}

// A subagent whose tier already puts it on the model set isn't marked as
// moved.
func TestDesktopSubagentResolvedModel(t *testing.T) {
	u := desktopSubagentSetup(t)
	if err := SetDesktopTier("haiku", "fake/m2"); err != nil {
		t.Fatal(err)
	}
	desktopSubagentSet(t, "fake/m2")
	s := New()
	desktopSubagentAsk(t, s, Token+"-claude-desktop", "claude-haiku-4-5-20251001", "worker", "/v1/messages", true)
	if got, _ := u.seen(); got != "m2" {
		t.Fatalf("tier sent %q", got)
	}
	if r := lastRoute(s); r.Subagent != nil {
		t.Fatalf("already resolved to target, traced %+v", r.Subagent)
	}
}

// A subagent on a tier (Claude Code's own haiku id, which the tier picked
// in uni-switch serves) goes on the model set all the same.
func TestDesktopSubagentTierMoved(t *testing.T) {
	u := desktopSubagentSetup(t)
	if err := SetDesktopTier("haiku", "fake/m1"); err != nil {
		t.Fatal(err)
	}
	desktopSubagentSet(t, "fake/m2")
	s := New()
	const named = "claude-haiku-4-5-20251001"
	desktopSubagentAsk(t, s, Token+"-claude-desktop", named, "worker", "/v1/messages", true)
	if got, _ := u.seen(); got != "m2" {
		t.Fatalf("tier subagent sent %q, want m2", got)
	}
	if r := lastRoute(s); r.Model != named || r.Subagent == nil || r.Subagent.Asked != named || r.Subagent.To != "fake/m2" {
		t.Fatalf("named tier trace: model %q subagent %+v", r.Model, r.Subagent)
	}
}

// The model set is read on each request: a change applies to the next
// subagent turn through the same server, without a restart.
func TestDesktopSubagentNextRequest(t *testing.T) {
	u := desktopSubagentSetup(t)
	// An overlapping request may still hold the catalog while the setting
	// changes. The next request must see that change through the same server.
	release := provider.Hold()
	defer release()
	s := New()
	for _, target := range []string{"fake/m2", "fake/m3", ""} {
		desktopSubagentSet(t, target)
		desktopSubagentAsk(t, s, Token+"-claude-desktop", "fake/m1", "worker", "/v1/messages", true)
		want := strings.TrimPrefix(target, "fake/")
		if want == "" {
			want = "m1"
		}
		if got, _ := u.seen(); got != want {
			t.Fatalf("next request sent %q, want %q", got, want)
		}
	}
}

// The model set never becomes the session's model, which Desktop's titles
// go to, and neither does a subagent's own.
func TestDesktopSubagentKeepsLeadPick(t *testing.T) {
	desktopSubagentSetup(t)
	s := New()
	desktopSubagentAsk(t, s, Token+"-claude-desktop", "fake/m1", "", "/v1/messages", true)
	desktopSubagentSet(t, "fake/m2")
	desktopSubagentAsk(t, s, Token+"-claude-desktop", "fake/m1", "worker", "/v1/messages", true)
	if b, err := os.ReadFile(desktopPickedPath()); err != nil || strings.TrimSpace(string(b)) != "fake/m1" {
		t.Fatalf("lead picked model %q: %v", b, err)
	}
	desktopSubagentAsk(t, s, Token+"-claude-desktop", "fake/m3", "worker", "/v1/messages", true)
	if b, err := os.ReadFile(desktopPickedPath()); err != nil || strings.TrimSpace(string(b)) != "fake/m1" {
		t.Fatalf("different subagent replaced lead pick with %q: %v", b, err)
	}
}

// count_tokens counts a subagent's turn on the model it will go to, and
// the lead's and a helper's on their own.
func TestDesktopSubagentCountTokens(t *testing.T) {
	u := desktopSubagentSetup(t)
	desktopSubagentSet(t, "fake/m2")
	s := New()
	for _, sub := range []string{"worker", ""} {
		desktopSubagentAsk(t, s, Token+"-claude-desktop", "fake/m1", sub, "/v1/messages/count_tokens", true)
		want := "m1"
		if sub != "" {
			want = "m2"
		}
		if got, path := u.seen(); got != want || path != "/v1/messages/count_tokens" {
			t.Fatalf("count sent %q to %q, want %s", got, path, want)
		}
	}
	desktopSubagentAsk(t, s, Token+"-claude-desktop", aliasFor("fake/m1"), "worker", "/v1/messages/count_tokens", true)
	if got, _ := u.seen(); got != "m2" {
		t.Fatalf("alias count sent %q, want m2", got)
	}
	desktopSubagentAsk(t, s, Token+"-claude-desktop", "fake/m1", "worker", "/v1/messages/count_tokens", false)
	if got, _ := u.seen(); got != "m1" {
		t.Fatalf("helper count sent %q, want m1", got)
	}
}

// An unset setting takes only the held settings read: decoding tools or
// building the served catalog would allocate on every ordinary request.
func TestDesktopSubagentUnsetDoesNotDecode(t *testing.T) {
	desktopSubagentSetup(t)
	release := provider.Hold()
	defer release()
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.Header.Set("x-claude-code-agent-id", "worker")
	body := []byte(`{"tools":[{"name":"Bash"}]}`)
	provider.DesktopSubagentModel() // warm the held settings
	allocs := testing.AllocsPerRun(10, func() {
		if p := desktopSubagentPick(r, "claude-desktop", "fake/m1", "fake/m1", body); p != nil {
			t.Fatalf("unset pick %+v", p)
		}
	})
	if allocs != 0 {
		t.Fatalf("unset pick allocated %g times, want only the held settings read", allocs)
	}
}
