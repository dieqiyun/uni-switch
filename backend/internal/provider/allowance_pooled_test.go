package provider

import (
	"testing"
	"time"
)

// A model no window counts, on an account whose every window is some
// models' only — Antigravity's Gemini models that fetchAvailableModels
// gives no quota (#3737) beside Claude's own — is refused alone: nothing
// says the account is out, and Claude used up never stopped Gemini 4
// either (runking on Discord). With a window of the whole account, its
// refusal is still the account's.
func TestAllowancePooledModelNoWindowCounts(t *testing.T) {
	now := time.Now()
	week := now.Add(5 * 24 * time.Hour)
	agy := allowanceOf([]QuotaWindow{
		{Name: "Claude Sonnet 4.6", Model: "claude-sonnet-4-6", Family: "Claude", Used: 100, ResetsAt: &week},
		{Name: "Gemini 3 Flash", Model: "gemini-3-flash", Family: "Gemini", Used: 8, ResetsAt: &week},
		{Name: "5 hours", Pool: "Gemini", Used: 8, Aside: true},
	}, now)
	for _, m := range []string{"gemini-4-argon-high", "claude-sonnet-4-6", "gemini-3-flash"} {
		if !agy.Pooled(m, 100, now) {
			t.Errorf("Antigravity: %s refused rests the account", m)
		}
	}
	whole := Allowance{{Used: 30, Resets: week, Span: 7 * 24 * time.Hour}, {Used: 10, Resets: week, Model: "opus"}}
	if whole.Pooled("gemini-4-argon-high", 100, now) {
		t.Error("a model the account's own week counts, refused, rests only the model")
	}
	if (Allowance{{Used: 10, Resets: week, Model: "opus"}, {Used: 20, Resets: week}}).Pooled("x", 100, now) {
		t.Error("with a window of the whole account, a model no window counts is refused alone")
	}
}
