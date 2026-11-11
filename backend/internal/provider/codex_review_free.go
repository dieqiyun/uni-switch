package provider

import "strings"

// CodexAutoReview is the reviewer model of OpenAI's that Codex asks its
// auto-review of an approval (the guardian) on.
const CodexAutoReview = "codex-auto-review"

// ChatGPTReviewFree says whether a call on model by a ChatGPT account on
// plan is free, counted in none of its usage windows and charged nothing:
// Codex's reviewer model, codex-auto-review, since OpenAI made Auto-review
// free for every user signed in through a ChatGPT account (2026-10-06,
// #1593, hywang1230; thsottiaux's "Day 2.1"), but not on a Free plan,
// where cybercaius on Discord measured it spending the allowance. A
// review run on another model (one that follows the session, or a stand-in
// uni-switch routes it to), or through an API key, is a call like any other.
func ChatGPTReviewFree(model, plan string) bool {
	return strings.EqualFold(strings.TrimSpace(model), CodexAutoReview) && !strings.EqualFold(strings.TrimSpace(plan), "free")
}

// codexReviewFree says whether a call on model through p costs nothing:
// p is the Codex subscription, which only ever holds ChatGPT sign-ins (an
// API key's auth.json is no account of it, codexAccount), and the call is
// one ChatGPTReviewFree. The plan is the one Codex is signed in to now; a
// Codex uni-switch can't read the plan of, or one signed out since the call,
// is taken as a paid plan, as the gateway's caps take it (capHeld). The
// ChatGPT API subscription (chatgpt-api) is not covered: OpenAI said it of
// Codex's own sign-in, not of its API.
func (p Provider) codexReviewFree(model string) bool {
	if p.ID != "codex" || p.Account != nil && p.Account.Agent != "codex" {
		return false
	}
	plan := ""
	if p.Account != nil {
		plan = p.Account.Plan
	}
	return ChatGPTReviewFree(model, plan)
}
