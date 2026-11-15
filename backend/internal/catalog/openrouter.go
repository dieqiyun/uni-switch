package catalog

import (
	"strconv"
	"strings"
)

// OpenRouterPricing is a price as OpenRouter's model list writes it
// (gateway /v1/models' and an agent plugin's connect's "pricing"), the
// shape agents and SDKs built on that list read: USD a token, as decimal
// strings. prompt and completion are always there, a price set at 0
// included, since the price is known; a cache read or write only when it
// has a price, as a part a list leaves at 0 is one it doesn't give. The
// base price only: a model billed more above a prompt size (Tiers) tells
// what it costs below it, and the 1-hour cache write has no field there.
func OpenRouterPricing(pr Price) map[string]string {
	out := map[string]string{"prompt": PerToken(pr.Input), "completion": PerToken(pr.Output)}
	if pr.CacheRead > 0 {
		out["input_cache_read"] = PerToken(pr.CacheRead)
	}
	if pr.CacheWrite > 0 {
		out["input_cache_write"] = PerToken(pr.CacheWrite)
	}
	return out
}

// PerToken is a price of USD a million tokens as USD a token, written as
// a decimal: the decimal point moved, not the float divided, so $0.3 is
// "0.0000003" and not 2.9999999999999997e-07.
func PerToken(perMillion float64) string {
	s := strconv.FormatFloat(max(perMillion, 0), 'f', -1, 64)
	whole, frac, _ := strings.Cut(s, ".")
	if len(whole) < 7 {
		whole = strings.Repeat("0", 7-len(whole)) + whole
	}
	cut := len(whole) - 6
	whole, frac = strings.TrimLeft(whole[:cut], "0"), strings.TrimRight(whole[cut:]+frac, "0")
	if whole == "" {
		whole = "0"
	}
	if frac == "" {
		return whole
	}
	return whole + "." + frac
}
