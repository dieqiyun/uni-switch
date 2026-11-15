package agent

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// TestT3CodeAutoModeReview: T3 Code's uni-switch instance runs Claude Code on
// the gateway through its own environment, so it is told the same: the
// review off while none of the models it lists is reviewed, and on (not
// set) once one is.
func TestT3CodeAutoModeReview(t *testing.T) {
	home := syncHome(t)
	t.Setenv("T3CODE_HOME", "")
	path := filepath.Join(home, ".t3", "userdata", "settings.json")
	writeFile(t, path, `{}`)
	a := t3code(home)
	if err := a.Field("provider").Set(uniSwitchID); err != nil {
		t.Fatal(err)
	}
	review := func() (string, bool) {
		t.Helper()
		var file map[string]map[string]json.RawMessage
		if err := json.Unmarshal([]byte(readFile(path)), &file); err != nil {
			t.Fatal(err)
		}
		for _, v := range t3Format(t, file["providerInstances"][uniSwitchID]).Environment {
			if v.Name == claudeReviewEnv {
				return v.Value, true
			}
		}
		return "", false
	}
	if v, ok := review(); v != "0" || !ok {
		t.Fatalf("relay/glm-4.6 alone: %q %v, want 0", v, ok)
	}
	if err := provider.Save(provider.Provider{ID: "a", Name: "A", Anthropic: "http://127.0.0.1:1", Key: "k", Models: []string{"claude-sonnet-5"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if v, ok := review(); ok {
		t.Fatalf("with a/claude-sonnet-5 listed: %q, want none", v)
	}
}

// TestClaudeReviewedOnlyWhereMessagesReachClaude (nexnull on Discord,
// Claude Code 2.1.308 on DeepSeek through uni-switch v0.1.1318, still told
// "this session isn't eligible because your requests go through
// 127.0.0.1:3425"): a Claude model a provider serves only on Chat or
// Responses is asked translated, and a translation carries neither the
// safeguards nor their results, so it can't keep the review on. A relay
// with no Anthropic endpoint listing claude-sonnet-5 beside DeepSeek, or a
// Claude model the user set to be asked on chat, leaves the review off; the
// same model asked on the relay's Anthropic endpoint turns it back on.
func TestClaudeReviewedOnlyWhereMessagesReachClaude(t *testing.T) {
	home := syncHome(t)
	path := filepath.Join(home, ".claude", "settings.json")
	writeFile(t, path, `{}`)
	review := func() string { v, _ := edit.GetJSON(path, "env."+claudeReviewEnv); return v }
	a := claude(home)
	relay := provider.Provider{ID: "r", Name: "R", Chat: "http://127.0.0.1:1/v1", Key: "k", Models: []string{"deepseek-v4.1-flash", "claude-sonnet-5"}}
	if err := provider.Save(relay); err != nil {
		t.Fatal(err)
	}
	if err := a.Field("model").Set("r/deepseek-v4.1-flash"); err != nil {
		t.Fatal(err)
	}
	if claudeReviewed(nil, "r/claude-sonnet-5") {
		t.Error("a Claude model asked on chat alone counted as reviewed")
	}
	if review() != "0" {
		t.Fatalf("a relay without an Anthropic endpoint: %q, want 0", review())
	}

	// the relay's Anthropic endpoint reaches Claude: on
	relay.Anthropic = "http://127.0.0.1:1"
	if err := provider.Save(relay); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if review() != "" {
		t.Fatalf("Claude on the relay's Anthropic endpoint: %q, want unset", review())
	}

	// set to be asked on chat there: off again
	if err := provider.SetModelAPI("r/claude-sonnet-5", "chat"); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if review() != "0" {
		t.Fatalf("Claude set to be asked on chat: %q, want 0", review())
	}
	if err := provider.SetModelAPI("r/claude-sonnet-5", ""); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if review() != "" {
		t.Fatalf("back to the vendor's list: %q, want unset", review())
	}
}
