package gateway

import (
	"cmp"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// Claude's refusal with what its classifier said of it, streamed and
// whole, as Anthropic ends a turn its safety filter stopped before
// anything was said (DD on Discord).
var (
	anthropicRefusalDetailed = strings.Replace(anthropicRefusal, `"stop_reason":"refusal","stop_sequence":null}`,
		`"stop_reason":"refusal","stop_sequence":null,"stop_details":{"type":"refusal","category":"cyber","explanation":"flagged"}}`, 1)
	anthropicRefusalWhole = `{"id":"m1","type":"message","role":"assistant","model":"m","content":[],"stop_reason":"refusal","stop_sequence":null,"stop_details":{"type":"refusal","category":"cyber","explanation":"flagged"},"usage":{"input_tokens":3,"output_tokens":2}}`
	anthropicAnswerWhole  = `{"id":"m2","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"from b"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":4}}`
)

// refusalRetries sets Settings' RefusalRetries for the test.
func refusalRetries(t *testing.T, n int) {
	t.Helper()
	s := settings.Load()
	s.RefusalRetries = &n
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
}

// keyedClaude is a keyed Anthropic provider "a" with keys k1, k2, k3 in
// order, the ones in refusing answering every turn with Claude's refusal,
// the others with "from b"; it returns the keys asked, in order.
func keyedClaude(t *testing.T, refusing ...string) *[]string {
	t.Helper()
	var keys []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		k := cmp.Or(r.Header.Get("x-api-key"), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		keys = append(keys, k)
		refuse := false
		for _, x := range refusing {
			refuse = refuse || x == k
		}
		if !strings.Contains(string(b), `"stream":true`) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, map[bool]string{true: anthropicRefusalWhole, false: anthropicAnswerWhole}[refuse])
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, map[bool]string{true: anthropicRefusalDetailed, false: anthropicAnswer}[refuse])
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "a", Name: "A", Models: []string{"m"}, Anthropic: up.URL, Routing: provider.Ordered,
		Key: "k1", Keys: []provider.KeyAccount{{Key: "k2"}, {Key: "k3"}}}); err != nil {
		t.Fatal(err)
	}
	return &keys
}

// handedBack says body is Claude's refusal as the vendor said it, to an
// agent on Anthropic's Messages API: what Claude Code switches to its
// fallback model on, not uni-switch's 400.
func handedBack(code int, body string) bool {
	return code == 200 && strings.Contains(body, `"stop_reason":"refusal"`) && strings.Contains(body, `"category":"cyber"`) &&
		!strings.Contains(body, "refused this request")
}

const messagesAsk = `{"model":"a/m","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"hi"}]}`

// With RefusalRetries 0, a keyed Anthropic provider's refusal goes to
// Claude Code as Anthropic said it, streamed or whole, with its other keys
// never asked and the key that refused not set aside (DD on Discord: uni-switch
// moved on through the accounts and answered 400, so Claude Code never
// switched to its fallback model as it does on Anthropic's own API).
func TestRefusalRetriesNoneHandsClaudeCodeTheRefusal(t *testing.T) {
	for _, stream := range []bool{true, false} {
		fresh(t)
		refusalRetries(t, 0)
		keys := keyedClaude(t, "k1", "k2", "k3")
		s := New()
		ask := messagesAsk
		if !stream {
			ask = strings.Replace(ask, `"stream":true,`, "", 1)
		}
		code, body := sendTo(s, "/v1/messages", ask)
		if !handedBack(code, body) || strings.Join(*keys, ",") != "k1" {
			t.Fatalf("stream %v: status %d: %s (keys %v)", stream, code, body, *keys)
		}
		r := lastRoute(s)
		if len(r.Tries) != 1 || r.Tries[0].Fail != failRefused || r.Tries[0].Rest != nil {
			t.Fatalf("stream %v: tries: %+v", stream, r.Tries)
		}
		if _, ok := restOf("a"); ok {
			t.Fatalf("stream %v: a set aside by a refusal", stream)
		}
	}
}

// RefusalRetries counts the tries after the first refusal: at 1, two keys
// of three are asked; unset, as by default, every one is (#248), and the
// one that answers is what the agent gets.
func TestRefusalRetriesCountsTheTries(t *testing.T) {
	fresh(t)
	refusalRetries(t, 1)
	keys := keyedClaude(t, "k1", "k2", "k3")
	code, body := sendTo(New(), "/v1/messages", messagesAsk)
	if !handedBack(code, body) || strings.Join(*keys, ",") != "k1,k2" {
		t.Fatalf("status %d: %s (keys %v)", code, body, *keys)
	}

	fresh(t)
	keys = keyedClaude(t, "k1", "k2")
	code, body = sendTo(New(), "/v1/messages", messagesAsk)
	if code != 200 || !strings.Contains(body, "from b") || strings.Join(*keys, ",") != "k1,k2,k3" {
		t.Fatalf("unset: status %d: %s (keys %v)", code, body, *keys)
	}

	// nobody left by default either: the vendor's refusal, every key asked
	fresh(t)
	keys = keyedClaude(t, "k1", "k2", "k3")
	code, body = sendTo(New(), "/v1/messages", messagesAsk)
	if !handedBack(code, body) || strings.Join(*keys, ",") != "k1,k2,k3" {
		t.Fatalf("unset, all refusing: status %d: %s (keys %v)", code, body, *keys)
	}
}

// Codex, on the Responses API, is still told a refusal with nobody left as
// a 400 it doesn't ask again for (#248): an empty reply that looks whole
// it would ask for again and again. RefusalRetries holds for it as well.
func TestRefusalRetriesCodexStillTold400(t *testing.T) {
	fresh(t)
	refusalRetries(t, 0)
	keys := keyedClaude(t, "k1")
	code, body := sendTo(New(), "/v1/responses", `{"model":"a/m","stream":true,"input":[{"role":"user","content":[{"type":"input_text","text":"hi"}]}]}`)
	if code != 400 || !strings.Contains(body, "refused this request") || strings.Join(*keys, ",") != "k1" {
		t.Fatalf("status %d: %s (keys %v)", code, body, *keys)
	}
}

// A Claude subscription served through the claude CLI: at RefusalRetries 0
// the spare account isn't asked, and Claude Code has the refusal with its
// classifier's category, streamed and whole.
func TestRefusalRetriesClaudeSubscription(t *testing.T) {
	log := claudeSubscriptionRefusing(t)
	refusalRetries(t, 0)
	s := New()
	for _, ask := range []string{
		`{"model":"claude/claude-sonnet-5","max_tokens":50,"stream":true,"messages":[{"role":"user","content":"ping"}]}`,
		`{"model":"claude/claude-sonnet-5","max_tokens":50,"messages":[{"role":"user","content":"ping again"}]}`,
	} {
		os.Remove(log)
		code, body := sendTo(s, "/v1/messages", ask)
		b, _ := os.ReadFile(log)
		if !handedBack(code, body) || strings.Join(strings.Fields(string(b)), ",") != "own" {
			t.Fatalf("%s: status %d: %s (runs %q)", ask, code, body, b)
		}
		r := lastRoute(s)
		if len(r.Tries) != 1 || r.Tries[0].Fail != failRefused || r.Tries[0].Rest != nil {
			t.Fatalf("tries: %+v", r.Tries)
		}
	}
}
