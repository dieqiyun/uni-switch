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

type desktopEffortUpstream struct {
	mu   sync.Mutex
	body []byte
	path string
}

func (u *desktopEffortUpstream) seen() ([]byte, string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]byte(nil), u.body...), u.path
}

func desktopEffortSetup(t *testing.T) *desktopEffortUpstream {
	t.Helper()
	fresh(t)
	noRests()
	t.Cleanup(noRests)
	u := &desktopEffortUpstream{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		u.mu.Lock()
		u.body, u.path = b, r.URL.Path
		u.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/count_tokens") {
			io.WriteString(w, `{"input_tokens":12}`)
			return
		}
		io.WriteString(w, `{"id":"reply","type":"message","role":"assistant","model":"`+modelOf(b)+`","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":7,"output_tokens":1}}`)
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Key: "k", Anthropic: up.URL,
		Models: []string{"m1", "m2", "short", "plain"}}); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"m1", "m2"} {
		if err := provider.SetModelEfforts("fake/"+m, []string{"none", "low", "medium", "high", "xhigh", "max"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := provider.SetModelEfforts("fake/short", []string{"low", "high"}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelReasonless("fake/plain", true); err != nil {
		t.Fatal(err)
	}
	return u
}

func desktopEffortSet(t *testing.T, level string) {
	t.Helper()
	if err := provider.SetDesktopSubagentEffort(level); err != nil {
		t.Fatal(err)
	}
}

// Requests use the same subagent marker and adaptive thinking shape as
// Desktop's Claude Code. No real Desktop config or gateway is involved.
func desktopEffortAsk(t *testing.T, s *Server, agent, model, sub, path string, tools bool, thinking string) {
	t.Helper()
	body := `{"model":"` + model + `","messages":[{"role":"user","content":"task"}]`
	if !strings.HasSuffix(path, "/count_tokens") {
		body += `,"max_tokens":64000`
	}
	if tools {
		body += `,"tools":[{"name":"Bash","input_schema":{"type":"object"}}]`
	}
	if thinking != "" {
		body += `,` + thinking
	}
	body += `}`
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("x-api-key", Token+"-"+agent)
	req.Header.Set("User-Agent", desktopCodeUA)
	req.Header.Set("x-claude-code-session-id", "lead-session")
	if sub != "" {
		req.Header.Set("x-claude-code-agent-id", sub)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%s: %d %s", model, rec.Code, rec.Body)
	}
}

const desktopAdaptive = `"thinking":{"type":"adaptive"},"output_config":{"effort":"xhigh"}`

// Effort works without replacing the model, with a replacement, and when
// an unavailable replacement leaves the request on its original model.
func TestDesktopSubagentEffortIndependentOfModel(t *testing.T) {
	for _, target := range []string{"", "fake/m1", "fake/m2", "fake/missing"} {
		t.Run(target, func(t *testing.T) {
			u := desktopEffortSetup(t)
			desktopSubagentSet(t, target)
			desktopEffortSet(t, "low")
			s := New()
			desktopEffortAsk(t, s, "claude-desktop", "fake/m1", "worker", "/v1/messages", true, desktopAdaptive)
			b, _ := u.seen()
			wantModel := "m1"
			if target == "fake/m2" {
				wantModel = "m2"
			}
			if modelOf(b) != wantModel || requestEffort(provider.Anthropic, b) != "low" {
				t.Fatalf("model %s, effort %s; want %s at low", modelOf(b), requestEffort(provider.Anthropic, b), wantModel)
			}
			r := lastRoute(s)
			if r.Model != "fake/m1" || r.Effort != "xhigh" || r.SubagentEffort != "low" || r.Kind != "" || r.Tries[len(r.Tries)-1].Effort != "low" {
				t.Fatalf("requested and actual effort lost: %+v", r)
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
			if rec.Requested != "fake/m1" || rec.Effort != "low" || rec.Subagent != "worker" {
				t.Fatalf("ledger: requested %q effort %q subagent %q", rec.Requested, rec.Effort, rec.Subagent)
			}
		})
	}
}

// The lead, tool-less helpers and every other agent keep their effort.
func TestDesktopSubagentEffortScope(t *testing.T) {
	for _, tc := range []struct {
		name, agent, sub string
		tools            bool
	}{
		{"lead", "claude-desktop", "", true},
		{"blank marker", "claude-desktop", " ", true},
		{"helper", "claude-desktop", "worker", false},
		{"terminal", "claude", "worker", true},
		{"codex", "codex", "worker", true},
		{"other", "curl", "worker", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := desktopEffortSetup(t)
			desktopEffortSet(t, "low")
			s := New()
			desktopEffortAsk(t, s, tc.agent, "fake/m1", tc.sub, "/v1/messages", tc.tools, desktopAdaptive)
			b, _ := u.seen()
			if got := requestEffort(provider.Anthropic, b); got != "xhigh" {
				t.Fatalf("neighbor's effort changed to %q", got)
			}
			if lastRoute(s).SubagentEffort != "" {
				t.Fatal("neighbor recorded a subagent effort pick")
			}
		})
	}
}

// A shared SubagentPick can also come from Codex's headers. It does not
// establish that a request is a Desktop subagent's tool turn.
func TestDesktopSubagentEffortDoesNotTrustCodexPick(t *testing.T) {
	u := desktopEffortSetup(t)
	if err := provider.SetCodexSubagentModel("fake/m2"); err != nil {
		t.Fatal(err)
	}
	desktopEffortSet(t, "low")
	s := New()
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
		`{"model":"fake/m1","max_tokens":64000,"messages":[{"role":"user","content":"task"}],`+desktopAdaptive+`}`))
	req.Header.Set("x-api-key", Token+"-claude-desktop")
	req.Header.Set("User-Agent", "codex_cli_rs/0.159.0")
	req.Header.Set("x-openai-subagent", "collab_spawn")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	b, _ := u.seen()
	if rec.Code != 200 || requestEffort(provider.Anthropic, b) != "xhigh" {
		t.Fatalf("a Codex pick applied Desktop effort: %d %s", rec.Code, b)
	}
	if r := lastRoute(s); r.Subagent == nil || r.SubagentEffort != "" {
		t.Fatalf("unexpected pick: %+v, effort %q", r.Subagent, r.SubagentEffort)
	}
}

// Default preserves both a requested level and disabled thinking; an
// explicit pick can turn reasoning on or off.
func TestDesktopSubagentEffortDefaultAndNone(t *testing.T) {
	for _, tc := range []struct {
		pick, thinking, want, typ string
	}{
		{"", desktopAdaptive, "xhigh", "adaptive"},
		{"", `"thinking":{"type":"disabled"}`, "", "disabled"},
		{"high", "", "high", "enabled"},
		{"high", `"thinking":{"type":"disabled"}`, "high", "enabled"},
		{"none", desktopAdaptive, "", "disabled"},
	} {
		t.Run(tc.pick+tc.typ, func(t *testing.T) {
			u := desktopEffortSetup(t)
			desktopEffortSet(t, tc.pick)
			desktopEffortAsk(t, New(), "claude-desktop", "fake/m1", "worker", "/v1/messages", true, tc.thinking)
			b, _ := u.seen()
			var v struct{ Thinking struct{ Type string } }
			if err := json.Unmarshal(b, &v); err != nil {
				t.Fatal(err)
			}
			if got := requestEffort(provider.Anthropic, b); got != tc.want || v.Thinking.Type != tc.typ {
				t.Fatalf("effort %q, thinking %q; want %q, %q", got, v.Thinking.Type, tc.want, tc.typ)
			}
		})
	}
}

func TestDesktopSubagentEffortReasonless(t *testing.T) {
	u := desktopEffortSetup(t)
	desktopSubagentSet(t, "fake/plain")
	desktopEffortSet(t, "high")
	for _, path := range []string{"/v1/messages", "/v1/messages/count_tokens"} {
		desktopEffortAsk(t, New(), "claude-desktop", "fake/plain", "worker", path, true, "")
		b, _ := u.seen()
		if strings.Contains(string(b), `"thinking"`) || strings.Contains(string(b), `"output_config"`) {
			t.Fatalf("reasonless model was asked to think: %s", b)
		}
	}
}

func TestDesktopSubagentEffortNextRequestAndCount(t *testing.T) {
	u := desktopEffortSetup(t)
	s := New()
	release := provider.Hold()
	defer release()
	for _, level := range []string{"low", "high", ""} {
		desktopEffortSet(t, level)
		want := level
		if want == "" {
			want = "xhigh"
		}
		for _, path := range []string{"/v1/messages", "/v1/messages/count_tokens"} {
			desktopEffortAsk(t, s, "claude-desktop", "fake/m1", "worker", path, true, desktopAdaptive)
			b, gotPath := u.seen()
			if got := requestEffort(provider.Anthropic, b); got != want || gotPath != path {
				t.Fatalf("next request: effort %q at %q, want %q at %q", got, gotPath, want, path)
			}
		}
	}
}

func TestDesktopSubagentEffortUnmaskedCount(t *testing.T) {
	u := desktopEffortSetup(t)
	p, err := provider.Find("fake")
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(*p); err != nil {
		t.Fatal(err)
	}
	desktopEffortSet(t, "low")
	desktopEffortAsk(t, New(), "claude-desktop", "fake/m1", "worker", "/v1/messages/count_tokens", true, desktopAdaptive)
	b, _ := u.seen()
	if got := requestEffort(provider.Anthropic, b); got != "low" {
		t.Fatalf("unmasked count lost the pick: %q", got)
	}
}

// The override survives translation from Desktop's Messages protocol to
// a Responses or Chat provider, as a subagent on another vendor needs.
func TestDesktopSubagentEffortTranslated(t *testing.T) {
	for _, api := range []string{"responses", "chat"} {
		t.Run(api, func(t *testing.T) {
			s, up := fasted(t)
			if err := provider.SetModelAPI("oa/gpt-6.1-sol", api); err != nil {
				t.Fatal(err)
			}
			desktopSubagentSet(t, "oa/gpt-6.1-sol")
			desktopEffortSet(t, "low")
			desktopEffortAsk(t, s, "claude-desktop", "an/claude-opus-5-5", "worker", "/v1/messages", true, desktopAdaptive)
			_, path, _, body := up.last(t)
			if body["model"] != "gpt-6.1-sol" {
				t.Fatalf("translated to wrong model: %v", body["model"])
			}
			level := body["reasoning_effort"]
			if api == "responses" {
				if reasoning, ok := body["reasoning"].(map[string]any); ok {
					level = reasoning["effort"]
				}
				if path != "/v1/responses" {
					t.Fatalf("translated path %q", path)
				}
			}
			if level != "low" {
				t.Fatalf("translated effort %v, want low", level)
			}
		})
	}
}

func TestDesktopSubagentEffortUnsetDoesNotDecode(t *testing.T) {
	desktopEffortSetup(t)
	release := provider.Hold()
	defer release()
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.Header.Set("x-claude-code-agent-id", "worker")
	body := []byte(`{"tools":[{"name":"Bash"}]}`)
	provider.DesktopSubagentEffort()
	if n := testing.AllocsPerRun(10, func() {
		if desktopSubagentEffort(r, "claude-desktop", body, nil) != "" {
			t.Fatal("unset effort changed the request")
		}
	}); n != 0 {
		t.Fatalf("unset effort allocated %g times", n)
	}
}
