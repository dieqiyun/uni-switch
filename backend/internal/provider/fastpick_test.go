package provider

import (
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// A model is switched fast for an agent only when uni-switch can ask its vendor
// for a fast mode, never a routing group (its members say it), and is kept
// by agent through a rename of its provider (#954).
func TestSetFastPick(t *testing.T) {
	fastHome(t)
	if err := SetFastPick("Codex", "oa/gpt-6.1-sol", true); err != nil {
		t.Fatal(err)
	}
	if !IsFastPick("codex", "oa/gpt-6.1-sol") || IsFastPick("opencode", "oa/gpt-6.1-sol") {
		t.Fatalf("picks %v", settings.Load().FastPicks)
	}
	for _, id := range []string{"rl/claude-opus-5-5", "oa/nope", GroupPrefix + "f"} {
		if err := SetFastPick("codex", id, true); err == nil {
			t.Errorf("%s switched fast", id)
		}
	}
	if err := SetFastPick("", "oa/gpt-6.1-sol", true); err == nil {
		t.Error("no agent, switched fast")
	}
	if got := FastPicks("codex"); !slices.Equal(got, []string{"oa/gpt-6.1-sol"}) {
		t.Fatalf("picks %v", got)
	}
	// a GPT model on a provider the user added by its address (#1615)
	if err := SetFastPick("codex", "rl/gpt-6.1-sol", true); err != nil {
		t.Fatal(err)
	}
	if err := SetFastPick("codex", "rl/gpt-6.1-sol", false); err != nil {
		t.Fatal(err)
	}

	st := settings.Load()
	// an Ultrafast pick moves with it (#1954)
	st.UltraPicks = map[string][]string{"codex": {"oa/gpt-6.1-sol"}}
	if !renameModelPrefs(&st, "oa", "openai") {
		t.Fatal("the rename moved nothing")
	}
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	Changed()
	if !IsFastPick("codex", "openai/gpt-6.1-sol") || IsFastPick("codex", "oa/gpt-6.1-sol") {
		t.Fatalf("after the rename: %v", settings.Load().FastPicks)
	}
	if !IsUltraPick("codex", "openai/gpt-6.1-sol") || IsUltraPick("codex", "oa/gpt-6.1-sol") {
		t.Fatalf("after the rename: %v", settings.Load().UltraPicks)
	}

	// switching off is allowed whatever the model is now, and drops the agent
	if err := SetFastPick("codex", "openai/gpt-6.1-sol", false); err != nil {
		t.Fatal(err)
	}
	if _, ok := settings.Load().FastPicks["codex"]; ok {
		t.Fatalf("an agent with none kept: %v", settings.Load().FastPicks)
	}
	if _, ok := settings.Load().UltraPicks["codex"]; ok {
		t.Fatalf("Ultrafast kept with fast off: %v", settings.Load().UltraPicks)
	}
}
