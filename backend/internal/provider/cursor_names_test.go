package provider

import (
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// The context Cursor puts in a model's name is its Context, shown apart:
// the name goes without it, unless it is what tells two models apart.
func TestCursorNamesWithoutCapacity(t *testing.T) {
	in := []catalog.Model{
		{ID: "claude-opus-5-5", Name: "Claude Opus 5.5 1M", Context: 1_000_000},
		{ID: "gpt-5.6-sol", Name: "GPT-5.6 Sol (1M) Thinking", Context: 1_000_000},
		{ID: "gpt-5.5", Name: "GPT-5.5", Context: 200_000},
		{ID: "gpt-5.5-1m", Name: "GPT-5.5 1M", Context: 1_000_000},
		{ID: "grok-a", Name: "Grok 1M", Context: 1_000_000},
		{ID: "grok-b", Name: "Grok 2M", Context: 2_000_000},
		{ID: "composer-2.5", Name: "Composer 2.5", Context: 200_000},
		{ID: "only", Name: "1M", Context: 1_000_000},
	}
	var got []string
	for _, m := range withoutCursorCapacity(in) {
		got = append(got, m.ID+"|"+m.Name)
	}
	want := []string{
		"claude-opus-5-5|Claude Opus 5.5",
		"gpt-5.6-sol|GPT-5.6 Sol Thinking",
		"gpt-5.5|GPT-5.5",
		"gpt-5.5-1m|GPT-5.5 1M", // beside a GPT-5.5, the 1M is what tells it apart
		"grok-a|Grok 1M",        // both would be Grok
		"grok-b|Grok 2M",
		"composer-2.5|Composer 2.5",
		"only|1M", // nothing would be left
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if in[0].Name != "Claude Opus 5.5 1M" {
		t.Fatalf("the list given was changed: %q", in[0].Name)
	}
}
