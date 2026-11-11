package codexcat

import (
	"encoding/json"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

func overrides(t *testing.T, ms []catalog.Model) map[string]any {
	t.Helper()
	var got struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(Catalog(ms), &got); err != nil {
		t.Fatal(err)
	}
	out := map[string]any{}
	for _, m := range got.Models {
		v, ok := m["auto_review_model_override"]
		if !ok {
			v = "none"
		}
		out[m["slug"].(string)] = v
	}
	return out
}

// With settings.CodexAutoReview every entry uni-switch hands Codex — the ChatGPT
// account's own and uni-switch's — names that model as
// auto_review_model_override, which Codex's auto-review runs on (#938); with
// none set no entry has the field, and one uni-switch left in Codex's cache is
// taken out.
func TestCodexAutoReviewEntries(t *testing.T) {
	v1Home(t, `{"etag":"W/\"a\"","models":[{"slug":"gpt-6","base_instructions":"x"},{"slug":"gpt-7","base_instructions":"x","auto_review_model_override":"deepseek/old"},{"slug":"gpt-8","base_instructions":"x","auto_review_model_override":"codex-auto-review"}]}`)
	ms := []catalog.Model{
		{ID: "codex/gpt-6", Name: "GPT-6 · Codex"},
		{ID: "codex/gpt-7", Name: "GPT-7 · Codex"},
		{ID: "codex/gpt-8", Name: "GPT-8 · Codex"},
		{ID: "codex/gpt-9", Name: "GPT-9 · Codex"},
		{ID: "group/smart", Name: "smart"},
		{ID: "anthropic/claude-opus", Name: "Opus"},
	}
	off := overrides(t, ms)
	want := map[string]any{"codex/gpt-6": "none", "codex/gpt-7": "none", "codex/gpt-8": "codex-auto-review",
		"codex/gpt-9": "none", "group/smart": "none", "anthropic/claude-opus": "none"}
	for k, v := range want {
		if off[k] != v {
			t.Fatalf("unset: %s = %v, want %v (%v)", k, off[k], v, off)
		}
	}

	s := settings.Load()
	s.CodexAutoReview = "deepseek/deepseek-flash"
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	on := overrides(t, ms)
	for k := range want {
		if on[k] != "deepseek/deepseek-flash" {
			t.Fatalf("set: %s = %v (%v)", k, on[k], on)
		}
	}
}

// The setting must be a model's id.
func TestCodexAutoReviewChecked(t *testing.T) {
	v1Home(t, `{"models":[]}`)
	s := settings.Load()
	s.CodexAutoReview = "flash"
	if err := settings.Save(s); err == nil {
		t.Fatal("a value that is no model's id was saved")
	}
	s.CodexAutoReview = "  group/smart "
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	if got := settings.Load().CodexAutoReview; got != "group/smart" {
		t.Fatalf("saved %q", got)
	}
}

// Set to "session" (cybercaius on Discord: left to Codex, a conversation
// on uni-switch's model had its auto-review on codex-auto-review through the
// ChatGPT sign-in), each of uni-switch's entries names itself, so the review
// runs on the session's model; Codex's own entries are left as OpenAI
// gave them, and one uni-switch put there before is taken out.
func TestCodexAutoReviewFollowsTheSession(t *testing.T) {
	v1Home(t, `{"etag":"W/\"a\"","models":[{"slug":"gpt-6","base_instructions":"x"},{"slug":"gpt-7","base_instructions":"x","auto_review_model_override":"deepseek/old"},{"slug":"gpt-8","base_instructions":"x","auto_review_model_override":"codex-auto-review"}]}`)
	s := settings.Load()
	s.CodexAutoReview = settings.CodexAutoReviewSession
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	ms := []catalog.Model{
		{ID: "codex/gpt-6", Name: "GPT-6 · Codex"},
		{ID: "codex/gpt-7", Name: "GPT-7 · Codex"},
		{ID: "codex/gpt-8", Name: "GPT-8 · Codex"},
		{ID: "group/smart", Name: "smart"},
		{ID: "token/gpt-6.1-sol", Name: "GPT-6.1 Sol"},
	}
	got := overrides(t, ms)
	want := map[string]any{"codex/gpt-6": "none", "codex/gpt-7": "none", "codex/gpt-8": "codex-auto-review",
		"group/smart": "group/smart", "token/gpt-6.1-sol": "token/gpt-6.1-sol"}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %v, want %v (%v)", k, got[k], v, got)
		}
	}
}

// cybercaius on Discord: codex-auto-review, kept as the default for being
// free on a ChatGPT sign-in (#1593), is not free on a Free plan. With
// nothing picked and Codex signed in to one, the review follows the
// session; on another plan, or one uni-switch can't read, it stays Codex's
// pick, and a model the user picked is kept on any plan.
func TestCodexAutoReviewOnAFreePlan(t *testing.T) {
	v1Home(t, `{"etag":"W/\"a\"","models":[{"slug":"gpt-6","base_instructions":"x"},{"slug":"gpt-8","base_instructions":"x","auto_review_model_override":"codex-auto-review"}]}`)
	plan := "free"
	was := SignInPlan
	SignInPlan = func() string { return plan }
	t.Cleanup(func() { SignInPlan = was })
	ms := []catalog.Model{
		{ID: "codex/gpt-6", Name: "GPT-6 · Codex"},
		{ID: "codex/gpt-8", Name: "GPT-8 · Codex"},
		{ID: "token/gpt-6.1-sol", Name: "GPT-6.1 Sol"},
	}
	check := func(what string, want map[string]any) {
		t.Helper()
		got := overrides(t, ms)
		for k, v := range want {
			if got[k] != v {
				t.Fatalf("%s: %s = %v, want %v (%v)", what, k, got[k], v, got)
			}
		}
	}
	check("free", map[string]any{"codex/gpt-6": "none", "codex/gpt-8": "codex-auto-review", "token/gpt-6.1-sol": "token/gpt-6.1-sol"})
	if got := AutoReviewModel(); got != settings.CodexAutoReviewSession {
		t.Fatalf("free: in effect %q", got)
	}
	for _, plan = range []string{"plus", ""} {
		check("plan "+plan, map[string]any{"codex/gpt-6": "none", "codex/gpt-8": "codex-auto-review", "token/gpt-6.1-sol": "none"})
	}
	plan = "free"
	s := settings.Load()
	s.CodexAutoReview = "deepseek/deepseek-flash"
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	check("picked", map[string]any{"codex/gpt-6": "deepseek/deepseek-flash", "codex/gpt-8": "deepseek/deepseek-flash", "token/gpt-6.1-sol": "deepseek/deepseek-flash"})
}
