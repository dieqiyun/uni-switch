package catalog

import (
	"strings"
	"testing"
)

// sameModelAsBefore is SameModel as it was before its plain ids were
// spared modelGaps and the copy.
func sameModelAsBefore(id string) string {
	k := strings.ToLower(strings.TrimSpace(id))
	if i := strings.LastIndex(k, "/"); i >= 0 {
		k = k[i+1:]
	}
	k = strings.Trim(modelGaps.ReplaceAllString(k, "-"), "-")
	b := []byte(k)
	for i := 1; i+1 < len(b); i++ {
		if (b[i] == '.' || b[i] == 'p') && digit(b[i-1]) && digit(b[i+1]) {
			b[i] = '-'
		}
	}
	k = string(b)
	if i := strings.LastIndexAny(k, "-@"); i > 0 && snapshotDate(k[i+1:]) {
		k = k[:i]
	}
	return k
}

// The Providers page asks SameModel of every model of every list: a plain
// id is read without a copy, and every id reads as it did.
func TestSameModelPlainIDsAllocateNothing(t *testing.T) {
	for _, id := range []string{
		"claude-opus-5-5-20260801", "anthropic/claude-sonnet-5", "Claude-Opus-5.5", "deepseek-v4p1", "GPT-6 Sol",
		"gpt-6 sol", "a__b", "a--b", "-a-", "--", "", " ", "1.2.3", "v1.2p3", "x\tY\nz", "qwen3-coder-2507",
		"gpt-4o-2024-08-06", "deepseek-v4-1-flash-260910", "m@20260801", "a-.-b", "1p", "p1", "1.p2", "kimi-k2-0905",
		"accounts/fireworks/models/deepseek-v4p1", "a　b", "a -b", "a\vb", "_lead", "trail_",
	} {
		if got, want := SameModel(id), sameModelAsBefore(id); got != want {
			t.Errorf("SameModel(%q) = %q, was %q", id, got, want)
		}
	}
	for _, id := range []string{"claude-opus-5-5-20260801", "gpt-6-sol", "deepseek-v4-1-flash"} {
		if n := testing.AllocsPerRun(100, func() { SameModel(id) }); n != 0 {
			t.Errorf("SameModel(%q) allocated %v times", id, n)
		}
	}
}
