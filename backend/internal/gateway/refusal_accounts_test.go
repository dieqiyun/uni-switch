package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// OpenAI's safety filter refusing a Codex turn, as the ChatGPT backend said
// it to Koohoko (#248): a 400 invalid_request_error with code bio_policy.
const bioPolicy = `{"error":{"type":"invalid_request_error","code":"bio_policy","message":"This content was flagged for possible biological risk. If this seems wrong, try rephrasing your request.","param":null}}`

// chatgptRefusing stands in for the ChatGPT backend with acct-1's turns
// refused by refuse, and every other account answering "pong"; it notes
// the accounts asked.
func chatgptRefusing(t *testing.T, tried *[]string, refuse func(w http.ResponseWriter), also ...string) {
	t.Helper()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		acct := r.Header.Get("chatgpt-account-id")
		*tried = append(*tried, acct)
		if acct == "acct-1" || slices.Contains(also, acct) {
			refuse(w)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`data: {"type":"response.created","response":{"id":"r1","model":"gpt-5.5"}}`,
			`data: {"type":"response.output_text.delta","delta":"pong"}`,
			`data: {"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":7,"output_tokens":1}}}`))
	}))
	t.Cleanup(up.Close)
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })
}

// codexAskOn is codexPost, on the given server.
func codexAskOn(s *Server, body string) (int, string) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer chatgpt-token")
	req.Header.Set("chatgpt-account-id", "acct-1")
	s.Handler().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

var refusing400 = func(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(400)
	io.WriteString(w, bioPolicy)
}

// A Codex subscription with two accounts in order: the first's safety
// filter refuses the turn — a 400 bio_policy, a stream failing with it, or
// Responses' own error event — and the second is asked and answers, both
// tries on the Routing page, neither account set aside (#248). Before, the
// 400 went to Codex with the second account never asked.
func TestPolicyRefusalMovesToNextAccount(t *testing.T) {
	for _, x := range []struct {
		name   string
		refuse func(w http.ResponseWriter)
	}{
		{"status 400", refusing400},
		{"response.failed", func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, sse(`data: {"type":"response.created","response":{"id":"r0"}}`,
				`data: {"type":"response.failed","response":{"id":"r0","status":"failed","error":{"code":"bio_policy","message":"This content was flagged for possible biological risk."}}}`))
		}},
		{"error event", func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, sse(`data: {"type":"response.created","response":{"id":"r0"}}`,
				`data: {"type":"error","code":"bio_policy","message":"This content was flagged for possible biological risk.","param":null}`))
		}},
	} {
		t.Run(x.name, func(t *testing.T) {
			codexSignedIn(t, "spare@example.com")
			if err := provider.SetRouting("codex", provider.Ordered); err != nil {
				t.Fatal(err)
			}
			var tried []string
			chatgptRefusing(t, &tried, x.refuse)
			s := New()
			code, body := codexAskOn(s, `{"model":"gpt-5.5","stream":true,"input":"ping"}`)
			if code != 200 || !strings.Contains(body, "pong") || strings.Contains(body, "bio_policy") {
				t.Fatalf("status %d: %s", code, body)
			}
			if strings.Join(tried, ",") != "acct-1,acct-2" {
				t.Fatalf("tried %v", tried)
			}
			r := lastRoute(s)
			if len(r.Tries) != 2 || r.Tries[0].Fail != failRefused || r.Tries[0].Rest != nil || r.Tries[0].Status != 400 ||
				!strings.Contains(r.Tries[0].Error, "bio_policy") || r.Tries[1].Status != 200 || r.Status != 200 {
				t.Fatalf("tries: %+v", r.Tries)
			}
			for _, c := range []string{"codex", "codex@me@example.com", "codex@spare@example.com"} {
				if _, ok := restOf(c); ok {
					t.Fatalf("%s set aside by a refusal", c)
				}
			}
		})
	}
}

// With every account refusing, Codex is told the turn was refused, as a 400
// naming the vendor's reason, once each was asked, every try on the
// Routing page and none of them set aside.
func TestPolicyRefusalNobodyLeft(t *testing.T) {
	codexSignedIn(t, "spare@example.com")
	if err := provider.SetRouting("codex", provider.Ordered); err != nil {
		t.Fatal(err)
	}
	var tried []string
	chatgptRefusing(t, &tried, refusing400, "acct-2")
	s := New()
	code, body := codexAskOn(s, `{"model":"gpt-5.5","stream":true,"input":"ping"}`)
	if code != 400 || !strings.Contains(body, "refused this request") || !strings.Contains(body, "bio_policy") || strings.Join(tried, ",") != "acct-1,acct-2" {
		t.Fatalf("status %d: %s (tried %v)", code, body, tried)
	}
	r := lastRoute(s)
	if len(r.Tries) != 2 || r.Status != 400 {
		t.Fatalf("tries: %+v", r.Tries)
	}
	for _, try := range r.Tries {
		if try.Fail != failRefused || try.Rest != nil {
			t.Fatalf("tries: %+v", r.Tries)
		}
	}
	if _, ok := restOf("codex@spare@example.com"); ok {
		t.Fatal("the last account set aside by a refusal")
	}
}

// Codex with only its own sign-in has its turn relayed as it came, the
// ChatGPT backend's 400 with it.
func TestPolicyRefusalOneAccountRelayed(t *testing.T) {
	codexSignedIn(t)
	var tried []string
	chatgptRefusing(t, &tried, refusing400)
	code, body := codexAskOn(New(), `{"model":"gpt-5.5","stream":true,"input":"ping"}`)
	if code != 400 || !strings.Contains(body, "bio_policy") || len(tried) != 1 {
		t.Fatalf("status %d: %s (tried %v)", code, body, tried)
	}
}

// A keyed Anthropic provider with two keys in order, asked by Codex over a
// stream: Claude's stop_reason "refusal" on the first key, with nothing
// said, is answered by the second, both tries on the Routing page.
func TestClaudeRefusalMovesToNextKey(t *testing.T) {
	fresh(t)
	var keys []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		k := r.Header.Get("x-api-key") + r.Header.Get("Authorization")
		keys = append(keys, k)
		w.Header().Set("Content-Type", "text/event-stream")
		if strings.Contains(k, "k1") {
			io.WriteString(w, anthropicRefusal)
			return
		}
		io.WriteString(w, anthropicAnswer)
	}))
	defer up.Close()
	if err := provider.Save(provider.Provider{ID: "a", Name: "A", Models: []string{"m"}, Anthropic: up.URL, Routing: provider.Ordered,
		Key: "k1", Keys: []provider.KeyAccount{{Key: "k2"}}}); err != nil {
		t.Fatal(err)
	}
	s := New()
	code, body := sendTo(s, "/v1/responses", `{"model":"a/m","stream":true,"input":[{"role":"user","content":[{"type":"input_text","text":"hi"}]}]}`)
	if code != 200 || !strings.Contains(body, "from b") || len(keys) != 2 {
		t.Fatalf("status %d: %s (keys %v)", code, body, keys)
	}
	r := lastRoute(s)
	if len(r.Tries) != 2 || r.Tries[0].Fail != failRefused || r.Tries[0].Rest != nil || r.Tries[1].Status != 200 {
		t.Fatalf("tries: %+v", r.Tries)
	}
	recs := usage.Load(time.Time{})
	if len(recs) != 2 || recs[0].ProviderKeyID != provider.KeyID("k1") || recs[1].ProviderKeyID != provider.KeyID("k2") {
		t.Fatalf("refusal and answer must keep their own keys: %+v", recs)
	}
}

// claudeSubscriptionRefusing signs in two Claude subscription accounts,
// served through a claude CLI whose run on the first (its own sign-in)
// ends in stop_reason "refusal" with nothing said, category cyber in its
// stop_details (then Claude Code's own "unable to respond" result), and
// whose run on the spare answers "pong". It returns the file each run
// writes its token to: "own" for the first, tok-spare for the spare.
func claudeSubscriptionRefusing(t *testing.T) string {
	t.Helper()
	return claudeSubscriptionFirstSays(t, `    echo '{"type":"stream_event","event":{"type":"message_start","message":{"id":"m","model":"claude-sonnet-5","usage":{"input_tokens":1}}}}'
    echo '{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}'
    echo '{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":""}}}'
    echo '{"type":"stream_event","event":{"type":"content_block_stop","index":0}}'
    echo '{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber","explanation":null}},"usage":{"output_tokens":2}}}'
    echo '{"type":"stream_event","event":{"type":"message_stop"}}'
    echo '{"type":"result","subtype":"success","is_error":true,"result":"API Error: Claude Code is unable to respond to this request, which appears to violate our Usage Policy"}'
`)
}

// claudeSubscriptionFirstSays is claudeSubscriptionRefusing with the first
// account's run writing first, shell lines, for each turn. Control
// requests (the interactive mode's initialize) are acknowledged.
func claudeSubscriptionFirstSays(t *testing.T, first string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for Claude Code")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	restingUntil.Lock()
	restingUntil.m = map[string]time.Time{}
	restingUntil.Unlock()
	far := time.Now().Add(24 * time.Hour).UnixMilli()
	oauth := func(tok string) map[string]any {
		return map[string]any{"claudeAiOauth": map[string]any{"accessToken": tok, "refreshToken": "r-" + tok, "expiresAt": far, "subscriptionType": "max"}}
	}
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.WriteFile(filepath.Join(home, ".claude", ".credentials.json"), mustJSON(oauth("tok-primary")), 0o600)
	os.WriteFile(filepath.Join(home, ".claude.json"), mustJSON(map[string]any{
		"oauthAccount": map[string]any{"emailAddress": "me@example.com", "accountUuid": "u-me"}}), 0o600)
	os.MkdirAll(filepath.Dir(provider.Path()), 0o755)
	os.WriteFile(filepath.Join(filepath.Dir(provider.Path()), "logins.json"), mustJSON([]map[string]any{
		{"agent": "claude", "user": "me@example.com", "plan": "max", "on": true, "seen": time.Now(), "auth": oauth("tok-me")},
		{"agent": "claude", "user": "spare@example.com", "plan": "max", "on": true, "seen": time.Now(), "auth": oauth("tok-spare")},
	}), 0o600)
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	script := `#!/bin/sh
if [ "$1" = auth ]; then echo '{"loggedIn":true,"authMethod":"oauth_token","apiProvider":"firstParty"}'; exit 0; fi
TOK=; [ -n "$CLAUDE_CONFIG_DIR" ] && TOK=$(sed -n 's/.*"accessToken": *"\([^"]*\)".*/\1/p' "$CLAUDE_CONFIG_DIR/.credentials.json")
echo "${TOK:-own}" >> ` + log + `
while read -r line; do
  case $line in *'"type":"control_request"'*)
    # acknowledged as Claude Code does: the interactive mode's initialize
    id=$(printf '%s' "$line" | sed 's/.*"request_id":"\([^"]*\)".*/\1/')
    echo '{"type":"control_response","response":{"subtype":"success","request_id":"'$id'"}}'
    continue ;;
  esac
  if [ -z "$TOK" ] || [ "$TOK" = tok-me ]; then
` + first + `  else
    echo '{"type":"stream_event","event":{"type":"message_start","message":{"id":"m","model":"claude-sonnet-5","usage":{"input_tokens":1}}}}'
    echo '{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"pong"}}}'
    echo '{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}}'
    echo '{"type":"stream_event","event":{"type":"message_stop"}}'
    echo '{"type":"result","subtype":"success","is_error":false,"result":"pong"}'
  fi
done
`
	testenv.Program(t, filepath.Join(dir, "claude"), script)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	provider.ForgetAccounts()
	t.Cleanup(provider.ForgetAccounts)
	return log
}

// A Claude subscription with two accounts, served through the claude CLI:
// the first's run ends in stop_reason "refusal" with nothing said (then
// Claude Code's own "unable to respond" result), and the spare answers —
// streamed to Codex and to an Anthropic client, and not streamed.
func TestClaudeSubscriptionRefusalMovesToNextAccount(t *testing.T) {
	log := claudeSubscriptionRefusing(t)
	s := New()
	for _, ask := range []struct{ path, body string }{
		{"/v1/responses", `{"model":"claude/claude-sonnet-5","stream":true,"input":[{"role":"user","content":[{"type":"input_text","text":"hi"}]}]}`},
		{"/v1/messages", `{"model":"claude/claude-sonnet-5","max_tokens":50,"stream":true,"messages":[{"role":"user","content":"ping"}]}`},
		{"/v1/messages", `{"model":"claude/claude-sonnet-5","max_tokens":50,"messages":[{"role":"user","content":"ping again"}]}`},
	} {
		os.Remove(log)
		code, body := sendTo(s, ask.path, ask.body)
		b, _ := os.ReadFile(log)
		if code != 200 || !strings.Contains(body, "pong") || strings.Join(strings.Fields(string(b)), ",") != "own,tok-spare" {
			t.Fatalf("%s: status %d: %s (runs %q)", ask.path, code, body, b)
		}
		r := lastRoute(s)
		if len(r.Tries) != 2 || r.Tries[0].Fail != failRefused || r.Tries[0].Rest != nil || r.Tries[1].Status != 200 {
			t.Fatalf("%s: tries: %+v", ask.path, r.Tries)
		}
		// the classifier's category, on the try it refused (#1768)
		if ask.path == "/v1/messages" && !strings.Contains(r.Tries[0].Error, "category cyber") {
			t.Errorf("%s: refused try says %q", ask.path, r.Tries[0].Error)
		}
	}
}

// A Claude subscription account refusing a conversation of ~110K tokens:
// the spare account isn't asked, as it would be a new Claude Code told the
// whole conversation, all of it written to that account's cache again, for
// a refusal about the content it most likely gives too. A 900K
// conversation refused on one Pro account and told again on the next cost
// the user most of a 5-hour window (Aisiverto on X). The refusal is the
// agent's, the one try on the Routing page.
func TestClaudeSubscriptionBigRefusalNotToldAgain(t *testing.T) {
	log := claudeSubscriptionRefusing(t)
	s := New()
	big := strings.Repeat("the conversation so far ", 440_000/24)
	for _, stream := range []bool{true, false} {
		os.Remove(log)
		body, _ := json.Marshal(map[string]any{"model": "claude/claude-sonnet-5", "max_tokens": 50, "stream": stream,
			"messages": []map[string]any{{"role": "user", "content": big}}})
		code, reply := sendTo(s, "/v1/messages", string(body))
		b, _ := os.ReadFile(log)
		if runs := strings.Join(strings.Fields(string(b)), ","); runs != "own" {
			t.Fatalf("stream %v: runs %q, want the refusing account's alone (status %d)", stream, runs, code)
		}
		// the agent is told the refusal, not an empty reply
		if strings.Contains(reply, "pong") || !strings.Contains(reply, "refusal") && !strings.Contains(reply, "Usage Policy") {
			t.Fatalf("stream %v: status %d: %s", stream, code, reply)
		}
		if r := lastRoute(s); len(r.Tries) != 1 {
			t.Fatalf("stream %v: tries: %+v", stream, r.Tries)
		}
	}
}

// Which errors are the safety filter's, and which are the request's own.
func TestPolicyRefusalShapes(t *testing.T) {
	for body, want := range map[string]bool{
		bioPolicy: true,
		`{"type":"invalid_request_error","code":"bio_policy","message":"flagged"}`:                                                           true,
		`{"error":{"code":"cyber_policy","message":"x"}}`:                                                                                    true,
		`{"error":{"code":"content_filter","message":"The response was filtered","innererror":{"code":"ResponsibleAIPolicyViolation"}}}`:     true,
		`{"error":{"code":"invalid_prompt","message":"Invalid prompt: your prompt was flagged as potentially violating our usage policy."}}`: true,
		`{"error":{"code":"invalid_prompt","message":"Invalid prompt: missing field"}}`:                                                      false,
		`{"error":{"type":"invalid_request_error","message":"max_tokens: too large"}}`:                                                       false,
		`not json`: false,
	} {
		if _, got := policyRefusal([]byte(body)); got != want {
			t.Errorf("%s: %v", body, got)
		}
	}
}
