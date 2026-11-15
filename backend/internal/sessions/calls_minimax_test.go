package sessions

import (
	"path/filepath"
	"testing"
	"time"
)

// #1605 (LukeZ1988): MiniMax Code's calls reach the usage ledger. The
// sessions are @minimax-ai/code 0.6.8's own, written in a sandbox against a
// fake Anthropic upstream: one session with a single call through uni-switch's
// entry, one with two through uni-switch's entry and then one on a relay of the
// user's own (custom_provider:relay, model m2). The calls through uni-switch are
// the gateway's to count; the relay's is MiniMax Code's own, read here.
func TestMiniMaxCodeCalls(t *testing.T) {
	for _, viaEnv := range []bool{false, true} {
		name := "~/.minimax"
		if viaEnv {
			name = "MINIMAX_DATA_DIR"
		}
		t.Run(name, func(t *testing.T) {
			claude, _ := setup(t)
			dir := filepath.Join(filepath.Dir(claude), "home", ".minimax")
			if viaEnv {
				dir = filepath.Join(filepath.Dir(claude), "elsewhere")
				t.Setenv("MINIMAX_DATA_DIR", dir)
			}
			copyTree(t, "testdata/minimax", dir)
			var got []Call
			for _, c := range Calls(time.Time{}) {
				if c.Agent == "minimax-code" {
					got = append(got, c)
				}
			}
			if len(got) != 1 {
				t.Fatalf("want MiniMax Code's one call on its own relay, got %d: %+v", len(got), got)
			}
			c := got[0]
			if c.Model != "m2" || c.Upstream != "custom_provider:relay" {
				t.Errorf("model %q upstream %q", c.Model, c.Upstream)
			}
			if c.Session != "mvs_82c610ef258f4350b76250bda12cacb5" {
				t.Errorf("session %q", c.Session)
			}
			if want := (Tokens{Input: 123, Output: 7, CacheRead: 50, CacheWrite: 20}); c.Tokens != want {
				t.Errorf("tokens %+v, want %+v", c.Tokens, want)
			}
			if !c.Time.Equal(time.UnixMilli(1791717506102)) {
				t.Errorf("time %v", c.Time)
			}
			if c.Msg == "" {
				t.Error("no message id to count the reply once by")
			}
		})
	}
}
