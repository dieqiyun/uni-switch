package provider

import (
	"fmt"

	"regexp"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// parseCursorModels reads `cursor-agent models` as it prints it, the way the
// retired built-in did: the tests below build their lists from it.
func parseCursorModels(out string) []catalog.Model {
	line := regexp.MustCompile(`^([A-Za-z0-9][\w.:-]*) - (.+)$`)
	var ms []catalog.Model
	for _, l := range strings.Split(ansi.ReplaceAllString(out, ""), "\n") {
		m := line.FindStringSubmatch(strings.TrimSpace(l))
		if m == nil {
			continue
		}
		name := strings.Join(strings.Fields(strings.ReplaceAll(m[2], "\u200b", "")), " ")
		name = strings.TrimSpace(strings.TrimSuffix(name, "(default)"))
		name = strings.TrimSpace(strings.TrimSuffix(name, "(current)"))
		ms = append(ms, catalog.Model{ID: m[1], Name: name, Context: cursorContext(m[1], name)})
	}
	return ms
}

// cursorListed is some of `cursor-agent models`, as it prints it.
const cursorListed = "Available models\n\nauto - Auto (default)\n" +
	"gpt-5.3-codex-low - Codex 5.3 Low\ngpt-5.3-codex - Codex 5.3\ngpt-5.3-codex-high - Codex 5.3 High\ngpt-5.3-codex-high-fast - Codex 5.3 High Fast\n" +
	"grok-4.7-low - Grok 4.7  Low\ngrok-4.7-low-fast - Grok 4.7  Low Fast​​\ngrok-4.7-medium - Grok 4.7  Medium\ngrok-4.7-medium-fast - Grok 4.7  Medium Fast​​\n" +
	"grok-4.7-xhigh - Grok 4.7  Extra High\ngrok-4.7-xhigh-fast - Grok 4.7  Extra High Fast​​\n" +
	"claude-opus-4-7-low - Claude Opus 4.7 1M Low\nclaude-opus-4-7-xhigh - Claude Opus 4.7 1M\nclaude-opus-4-7-max - Claude Opus 4.7 1M Max\n" +
	"claude-opus-4-7-thinking-low - Claude Opus 4.7 1M Low Thinking\nclaude-opus-4-7-thinking-xhigh - Claude Opus 4.7 1M Thinking\n" +
	"claude-4.6-opus-high-thinking - Claude Opus 4.6 1M Thinking\nclaude-4.6-opus-max-thinking - Claude Opus 4.6 1M Max Thinking\n" +
	"gpt-5.5-none - GPT-5.5 1M None\ngpt-5.5-medium - GPT-5.5 1M\ngpt-5.5-extra-high - GPT-5.5 1M Extra High\n" +
	"gpt-5.5-medium-fast - GPT-5.5 Fast\ngpt-5.5-extra-high-fast - GPT-5.5 Extra High Fast\n" +
	"claude-4.6-sonnet-medium-thinking - Claude Sonnet 4.6 1M Thinking\ncomposer-2.5 - Composer 2.5\n\nTip: use --model <id>\n"

// Each family of Cursor's ids is one model, with the efforts it has; fast
// and thinking are families of their own, and a family of one keeps its id.
func TestCollapseCursorModels(t *testing.T) {
	raw := withCursorContexts(parseCursorModels(cursorListed))
	if raw[5].Name != "Grok 4.7 Low" || raw[6].Name != "Grok 4.7 Low Fast" {
		t.Fatalf("%q %q", raw[5].Name, raw[6].Name)
	}
	var got []string
	for _, m := range collapseCursorModels(raw) {
		got = append(got, fmt.Sprintf("%s|%s|%d|%s", m.ID, m.Name, m.Context, strings.Join(m.Efforts, ",")))
	}
	want := []string{
		"auto|Auto|200000|",
		"gpt-5.3-codex|Codex 5.3|200000|low,high",
		"gpt-5.3-codex-high-fast|Codex 5.3 High Fast|200000|",
		"grok-4.7|Grok 4.7|200000|low,medium,xhigh",
		"grok-4.7-fast|Grok 4.7 Fast|200000|low,medium,xhigh",
		"claude-opus-4-7|Claude Opus 4.7 1M|1000000|low,xhigh,max",
		"claude-opus-4-7-thinking|Claude Opus 4.7 1M Thinking|1000000|low,xhigh",
		"claude-4.6-opus-thinking|Claude Opus 4.6 1M Thinking|1000000|high,max",
		"gpt-5.5|GPT-5.5 1M|1000000|none,medium,xhigh",
		"gpt-5.5-fast|GPT-5.5 Fast|200000|medium,xhigh", // Cursor doesn't say 1M of it
		"claude-4.6-sonnet-medium-thinking|Claude Sonnet 4.6 1M Thinking|1000000|",
		"composer-2.5|Composer 2.5|200000|",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, c := range []struct{ model, effort, want string }{
		{"gpt-5.3-codex", "", "gpt-5.3-codex"},
		{"grok-4.7", "", "grok-4.7-medium"},
		{"claude-opus-4-7", "", "claude-opus-4-7-xhigh"}, // its name doesn't say its effort
		{"claude-opus-4-7-thinking", "low", "claude-opus-4-7-thinking-low"},
		{"claude-4.6-opus-thinking", "max", "claude-4.6-opus-max-thinking"},
		{"gpt-5.5", "xhigh", "gpt-5.5-extra-high"},
		{"gpt-5.5-fast", "", "gpt-5.5-medium-fast"},
	} {
		vs, ok := cursorVariantsIn(raw, c.model)
		if !ok || vs[c.effort] != c.want {
			t.Errorf("%s at %q: %v", c.model, c.effort, vs)
		}
	}
	for _, id := range []string{"composer-2.5", "gpt-5.3-codex-high-fast", "grok-4.7-low", "nope"} {
		if _, ok := cursorVariantsIn(raw, id); ok {
			t.Errorf("%s stands for a family", id)
		}
	}
}

// Cursor's names say which of its models hold 1M; the rest hold its 200K
func TestCursorContext(t *testing.T) {
	for _, c := range []struct {
		id, name string
		want     int
	}{
		{"claude-opus-5-5-high-fast", "Claude Opus 5.5 1M High Fast", 1_000_000},
		{"gpt-5.6-sol-xhigh", "GPT-5.6 Sol 1M Extra High", 1_000_000},
		{"gpt-5.5-medium-fast", "GPT-5.5 Fast", 200_000},
		{"composer-2.5", "Composer 2.5", 200_000},
		{"auto", "Auto", 200_000},
	} {
		if got := cursorContext(c.id, c.name); got != c.want {
			t.Errorf("%s (%s): %d, want %d", c.id, c.name, got, c.want)
		}
	}
	if ms := withCursorContexts(parseCursorModels("x-1 - X 1M\n")); ms[0].Context != 1_000_000 {
		t.Fatalf("%+v", ms)
	}
}

// Served by the plugin, a pick of Cursor's own id stays that id: the list
// the retired built-in left in the cache doesn't fold it into a family the
// plugin may send as is (ysm119: claude-opus-5-5-low picked, shown and
// sent as claude-opus-5-5, "AI Model Not Found").
func TestCursorPicksKeptWhenPluginServes(t *testing.T) {
	if !PluginServes("cursor") {
		t.Fatal("cursor isn't plugin-served")
	}
	raw := []catalog.Model{
		{ID: "claude-opus-5-5-low", Name: "Claude Opus 5.5 1M Low"},
		{ID: "claude-opus-5-5-medium", Name: "Claude Opus 5.5 1M"},
		{ID: "claude-opus-5-5-high", Name: "Claude Opus 5.5 1M High"},
		{ID: "claude-opus-5-5-low-fast", Name: "Claude Opus 5.5 1M Low Fast"},
		{ID: "claude-opus-5-5-medium-fast", Name: "Claude Opus 5.5 1M Fast"},
	}
	if err := catalog.SaveLive("cursor", "", raw); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.SaveLive("cursor", "", nil) })
	// the stale list does fold them: what the guard keeps out
	if b, _, ok := cursorBaseIn(raw, "claude-opus-5-5-low"); !ok || b != "claude-opus-5-5" {
		t.Fatalf("fixture doesn't fold: %q %v", b, ok)
	}
	picks := []string{"claude-opus-5-5-low", "claude-opus-5-5-medium"}
	if got := cursorPicks(picks); fmt.Sprint(got) != fmt.Sprint(picks) {
		t.Errorf("picks = %v, want %v", got, picks)
	}
	if b, e, ok := CursorBase("claude-opus-5-5-low"); ok {
		t.Errorf("CursorBase = %q %q, want none", b, e)
	}
	if vs, ok := CursorVariants("claude-opus-5-5"); ok {
		t.Errorf("CursorVariants = %v, want none", vs)
	}
}
