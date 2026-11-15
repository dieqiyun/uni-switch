package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// claudeCaching has the fake Claude Code of claudeMadeFirst answer on both
// accounts, each healthy, and report its prompt cache as Claude Code's
// stream does for a subscription: the conversation written for an hour.
// A turn whose text holds SMALL reports a small, 5-minute write only.
func claudeCaching(t *testing.T) {
	t.Helper()
	claudeMadeFirst(t, false)
	reset := strconv.FormatInt(time.Now().Add(3*time.Hour).Unix(), 10)
	script := `#!/bin/sh
case "$1" in auth) exit 1;; esac
creds="${CLAUDE_CONFIG_DIR:-$HOME/.claude}/.credentials.json"
if [ "$2" = "/usage" ]; then
  echo '{"type":"result","is_error":false,"result":"Current session: 10% used"}'
  exit 0
fi
n=0
while read -r line; do
  case $line in *'"type":"control_request"'*)
    id=$(printf '%s' "$line" | sed 's/.*"request_id":"\([^"]*\)".*/\1/')
    echo '{"type":"control_response","response":{"subtype":"success","request_id":"'$id'"}}'
    continue ;;
  esac
  n=$((n+1))
  tok=$(grep -o 'tok-[a-z]*' "$creds" | head -1)
  usage='"input_tokens":5,"cache_read_input_tokens":20000,"cache_creation_input_tokens":300000,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":300000}'
  case $line in *SMALL*) usage='"input_tokens":5,"cache_read_input_tokens":0,"cache_creation_input_tokens":300' ;; esac
  echo '{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","rateLimitType":"five_hour","utilization":0.1,"resetsAt":` + reset + `}}'
  echo '{"type":"stream_event","event":{"type":"message_start","message":{"id":"m","model":"claude-opus-5-5","usage":{'"$usage"'}}}}'
  echo '{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"'$tok' pid '$$' turn '$n'"}}}'
  echo '{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}}'
  echo '{"type":"stream_event","event":{"type":"message_stop"}}'
  echo '{"type":"result","subtype":"success","is_error":false,"result":""}'
done
`
	bin := strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))[0]
	testenv.Program(t, filepath.Join(bin, "claude"), script)
}

// A conversation on one of two Claude accounts goes on on that account, in
// the Claude Code that had its last turn, so Anthropic's cache of it — the
// account's own — is read again rather than the whole conversation written
// on the other account (slkassinthehood on X: two Pro accounts' 5 hours
// spent in ten minutes).
func TestClaudeConversationKeepsItsAccountsCache(t *testing.T) {
	claudeCaching(t)
	restingUntil.Lock()
	restingUntil.m = map[string]time.Time{}
	restingUntil.Unlock()
	p, err := provider.Find("claude")
	if err != nil {
		t.Fatal(err)
	}
	p.Routing = provider.LeastUsed
	if err := provider.Save(*p); err != nil {
		t.Fatal(err)
	}
	s := New()
	t.Cleanup(s.subscription.abortAll)
	h := s.Handler()
	sendAs := func(session, agent, msgs string) string {
		t.Helper()
		body := `{"model":"claude/claude-opus-5-5","max_tokens":100,"tools":[{"name":"read","input_schema":{"type":"object"}}],"messages":` + msgs + `}`
		r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
		r.Header.Set("x-claude-code-session-id", session)
		if agent != "" {
			// as Claude Code marks a subagent's requests; its lead's carry none
			r.Header.Set("x-claude-code-agent-id", agent)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		var res struct {
			Content []struct{ Text string } `json:"content"`
		}
		json.Unmarshal(rec.Body.Bytes(), &res)
		if rec.Code != 200 || len(res.Content) == 0 {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		a := s.trace.routes[len(s.trace.routes)-1].Affinity
		t.Logf("%q affinity %+v", res.Content[0].Text, a)
		return res.Content[0].Text
	}
	send := func(msgs string) string { t.Helper(); return sendAs("s-1", "", msgs) }
	msg := func(role, text string) string { return `{"role":"` + role + `","content":` + strconv.Quote(text) + `}` }
	age := func(d time.Duration) {
		sticks.Lock()
		for k, st := range sticks.m {
			st.at = st.at.Add(-d)
			if !st.hour.at.IsZero() {
				st.hour.at = st.hour.at.Add(-d)
			}
			sticks.m[k] = st
		}
		sticks.Unlock()
	}
	who := func(reply string) string { tok, _, _ := strings.Cut(reply, " pid"); return tok }
	run := func(reply string) string {
		_, rest, _ := strings.Cut(reply, " pid ")
		p, _, _ := strings.Cut(rest, " ")
		return p
	}

	conv := msg("user", "lead: fix the bug")
	first := send(`[` + conv + `]`)
	conv += `,` + msg("assistant", first)
	// a new conversation goes to the account that has served less
	if got := sendAs("s-2", "", `[`+msg("user", "SMALL another session")+`]`); who(got) == who(first) {
		t.Fatalf("a new conversation went to %q, not the less used account", got)
	}
	for i, step := range []struct {
		name string
		do   func()
	}{
		{"next turn", func() {}},
		{"after 20 minutes", func() { age(20 * time.Minute) }},
		{"after a sub-agent on the same model and session", func() {
			sendAs("s-1", "agent-1", `[`+msg("user", "SMALL subagent: look around")+`]`)
		}},
		{"after a sub-agent, 20 minutes on", func() {
			sendAs("s-1", "agent-2", `[`+msg("user", "SMALL subagent: look again")+`]`)
			age(20 * time.Minute)
		}},
	} {
		step.do()
		conv += `,` + msg("user", "and "+strconv.Itoa(i))
		got := send(`[` + conv + `]`)
		if who(got) != who(first) || run(got) != run(first) {
			t.Errorf("%s: answered %q, the conversation began as %q: moved off its cache", step.name, got, first)
		}
		conv += `,` + msg("assistant", got)
	}
}
