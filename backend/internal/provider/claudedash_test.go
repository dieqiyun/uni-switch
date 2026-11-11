package provider

import "testing"

// Only a Claude model's version is respelled: another vendor's dotted
// version, and a Claude id with no dot, are as they are.
func TestClaudeDashedOnlyClaudeVersions(t *testing.T) {
	for in, want := range map[string]string{
		"claude-opus-4.6":               "claude-opus-4-6",
		"relay/claude-opus-4.6-us[1m]":  "relay/claude-opus-4-6-us[1m]",
		"Claude-Sonnet-4.5":             "Claude-Sonnet-4-5",
		"claude-haiku-4.5-20251001":     "claude-haiku-4-5-20251001",
		"claude-opus-4-6":               "claude-opus-4-6",
		"gpt-5.1":                       "gpt-5.1",
		"relay/glm-4.6":                 "relay/glm-4.6",
		"gemini-2.5-pro":                "gemini-2.5-pro",
		"anthropic/claude-3.7-sonnet":   "anthropic/claude-3.7-sonnet",
		"relay/claude-opus-4.6:high":    "relay/claude-opus-4-6:high",
		"deepseek-v3.2-claude-opus-4.1": "deepseek-v3.2-claude-opus-4-1",
		"claude-mythos-5.1":             "claude-mythos-5-1",
	} {
		if got := ClaudeDashed(in); got != want {
			t.Errorf("ClaudeDashed(%q) = %q, want %q", in, got, want)
		}
	}
}

// A dashed spelling comes back to the one served id it stands for; an id
// served as it is spelled wins, and two ids that dash alike are neither.
func TestUndashInExactWins(t *testing.T) {
	es := func(ids ...string) (out []Entry) {
		for _, id := range ids {
			out = append(out, Entry{ID: id})
		}
		return out
	}
	for _, c := range []struct {
		served []string
		id     string
		want   string
	}{
		{[]string{"r/claude-opus-4.6-us", "r/gpt-5.1"}, "r/claude-opus-4-6-us", "r/claude-opus-4.6-us"},
		{[]string{"b/claude-opus-4.6", "b/claude-opus-4-6"}, "b/claude-opus-4-6", ""},
		{[]string{"b/claude-opus-4-6", "b/claude-opus-4.6"}, "b/claude-opus-4-6", ""},
		{[]string{"a/claude-opus-4.6.1", "a/claude-opus-4.6-1"}, "a/claude-opus-4-6-1", ""},
		{[]string{"r/claude-opus-4.6"}, "r/claude-opus-4-7", ""},
	} {
		got, ok := undashIn(es(c.served...), c.id)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("undashIn(%v, %q) = %q %v, want %q", c.served, c.id, got, ok, c.want)
		}
	}
}

func TestClaudeSplitKeepsEffortAndMark(t *testing.T) {
	for in, want := range map[string][2]string{
		"r/claude-opus-4-6-us[1m]":      {"r/claude-opus-4-6-us", "[1m]"},
		"r/claude-opus-4-6-us:high[1m]": {"r/claude-opus-4-6-us", ":high[1m]"},
		"r/claude-opus-4-6-us:high":     {"r/claude-opus-4-6-us", ":high"},
		"r/claude-opus-4-6-us":          {"r/claude-opus-4-6-us", ""},
	} {
		if id, rest := claudeSplit(in); id != want[0] || rest != want[1] {
			t.Errorf("claudeSplit(%q) = %q %q, want %q", in, id, rest, want)
		}
	}
}
