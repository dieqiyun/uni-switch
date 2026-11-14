package gateway

import (
	"encoding/json"

	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"

	"strings"

	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// An Antigravity family the user lists by an id of theirs goes out as the
// variant of the vendor's id the effort picks, not as the family's bare id
// a wire name gives it.
func TestLocalModelIDAtAnAntigravityAccount(t *testing.T) {
	fresh(t)
	if err := os.MkdirAll(filepath.Dir(catalog.LivePath("antigravity")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.LivePath("antigravity"), []byte(`{"models":[{"id":"flash-9-low"},{"id":"flash-9-high"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	s := settings.Load()
	s.ModelIDs = map[string]string{"antigravity/my-flash": "flash-9"}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	p := provider.Provider{ID: "antigravity", Account: &provider.Account{Agent: "antigravity", User: "u"}}
	r, err := parseChat([]byte(`{"model":"antigravity/my-flash","stream":true,"reasoning_effort":"low","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var env struct{ Model string }
	if err := json.Unmarshal(codeAssistBody(p, r, "my-flash", settings.Load().Wires()), &env); err != nil {
		t.Fatal(err)
	}
	if env.Model != "flash-9-low" {
		t.Fatalf("my-flash at low went out as %q, want flash-9-low", env.Model)
	}
}

// A Claude account's model the user lists by an id of theirs is run in
// Claude Code by Anthropic's id: no wire name reaches the run.
func TestLocalModelIDAtAClaudeAccount(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for Claude Code")
	}
	fresh(t)
	s := settings.Load()
	s.ModelIDs = map[string]string{"claude/my-sonnet": "claude-sonnet-5"}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	argv := filepath.Join(dir, "argv")
	testenv.Program(t, filepath.Join(dir, "claude"), `#!/bin/sh
echo "$@" > '`+argv+`'
read -r line
echo '{"type":"stream_event","event":{"type":"message_start","message":{"id":"m","model":"claude-sonnet-5","usage":{"input_tokens":1}}}}'
echo '{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}'
echo '{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}}'
echo '{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}}'
echo '{"type":"stream_event","event":{"type":"message_stop"}}'
`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	srv := New()
	t.Cleanup(srv.subscription.abortAll)
	p := provider.Provider{ID: "claude", Account: &provider.Account{Agent: "claude", User: "u"}}
	if own := p.OwnModelID("my-sonnet"); own != "claude-sonnet-5" {
		t.Fatalf("OwnModelID(my-sonnet) = %q", own)
	}
	body := `{"model":"claude/my-sonnet","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hi"}]}`
	rec := httptest.NewRecorder()
	var u Usage
	srv.serveClaudeSubscription(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)), provider.Anthropic, p, "my-sonnet", []byte(body), &u)
	b, err := os.ReadFile(argv)
	if err != nil {
		t.Fatalf("Claude Code never ran: %v; %s", err, rec.Body.String())
	}
	if args := string(b); !strings.Contains(args, "claude-sonnet-5") || strings.Contains(args, "my-sonnet") {
		t.Fatalf("Claude Code ran as: %s", args)
	}
}
