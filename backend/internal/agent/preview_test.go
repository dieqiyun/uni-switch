package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The lines Disconnect puts back, as the disconnect dialog shows them: a
// line taken out, one put in, a value going back; secrets masked; quick on
// a config of tens of thousands of lines (a Codex config.toml with its
// [projects]).
func TestDiffLines(t *testing.T) {
	var big strings.Builder
	for i := 0; i < 30000; i++ {
		fmt.Fprintf(&big, "[projects.\"/p/%d\"]\ntrust_level = \"trusted\"\n", i)
	}
	before := "model = \"uni-switch/deepseek/pro\"\nmodel_provider = \"uni-switch\"\napi_key = \"sk-live-123\"\n" + big.String() + "[model_providers.uni-switch]\nname = \"uni-switch\"\n"
	after := "model = \"gpt-6\"\nopenai_key = \"sk-live-456\"\n" + big.String()
	start := time.Now()
	got := diffLines(before, after)
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("took %s", d)
	}
	want := []LineDiff{
		{Op: "~", Text: `model = "gpt-6"`, Was: `model = "uni-switch/deepseek/pro"`},
		{Op: "+", Text: "openai_key = ••••"},
		{Op: "-", Text: `model_provider = "uni-switch"`},
		{Op: "-", Text: "api_key = ••••"},
		{Op: "-", Text: "[model_providers.uni-switch]"},
		{Op: "-", Text: `name = "uni-switch"`},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}

	// a key goes back to the line as deep as it, not one of the same name
	// in a table taken out (Claude's modelPicker)
	if d := diffLines("{\n  \"picker\": [\n    {\"x\": 1,\n    \"model\": \"a/b\"}\n  ],\n  \"model\": \"a/c\",\n}\n", "{\n  \"model\": \"opus\",\n}\n"); len(d) == 0 || d[0] != (LineDiff{Op: "~", Text: `  "model": "opus",`, Was: `  "model": "a/c",`}) {
		t.Fatalf("paired with a nested line: %v", d)
	}
	if d := diffLines("a\nb\n", "a\nb\n"); len(d) != 0 {
		t.Fatalf("same texts: %v", d)
	}
	if d := diffLines("", "x = 1\n}\n"); fmt.Sprint(d) != fmt.Sprint([]LineDiff{{Op: "+", Text: "x = 1"}}) {
		t.Fatalf("from nothing, a lone brace left out: %v", d)
	}
	// a block of thousands taken out, past what is compared line by
	// line: its lines, quickly
	var table strings.Builder
	for i := 0; i < 7000; i++ {
		fmt.Fprintf(&table, "  \"m%d\": {\"name\": \"M %d\"},\n", i, i)
	}
	start = time.Now()
	d := diffLines("{\n\"model\": \"uni-switch/x\",\n\"provider\": {\n"+table.String()+"}\n}\n", "{\n\"provider\": {\n}\n}\n")
	if time.Since(start) > 2*time.Second || len(d) != 7001 || d[0] != (LineDiff{Op: "-", Text: `"model": "uni-switch/x",`}) {
		t.Fatalf("a table taken out: %d lines in %s, first %v", len(d), time.Since(start), d[:min(len(d), 1)])
	}
}

func TestMask(t *testing.T) {
	const compact = `[{"requestHeaders":{"Authorization":"Bearer uni-switch"}}]`
	for in, want := range map[string]string{
		`  "ANTHROPIC_AUTH_TOKEN": "uni-switch",`:      `  "ANTHROPIC_AUTH_TOKEN": "uni-switch",`,
		`  "ANTHROPIC_API_KEY": "sk-ant-x",`:           `  "ANTHROPIC_API_KEY": ••••`,
		`export OPENAI_API_KEY=sk-1`:                   `export OPENAI_API_KEY= ••••`,
		`env_key = "$KEY"`:                             `env_key = "$KEY"`,
		`model = "x"`:                                  `model = "x"`,
		`"CLAUDE_CODE_MAX_CONTEXT_TOKENS": "1000000",`: `"CLAUDE_CODE_MAX_CONTEXT_TOKENS": "1000000",`,
		// A table header or compact array is not a key/value line.
		`[auth]`: `[auth]`,
		compact:  compact,
	} {
		if got := mask(in); got != want {
			t.Errorf("mask(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestElapsed(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"  01:02":     62 * time.Second,
		"03:00:01":    3*time.Hour + time.Second,
		"2-00:00:10":  48*time.Hour + 10*time.Second,
		"10-23:59:59": 10*24*time.Hour + 23*time.Hour + 59*time.Minute + 59*time.Second,
	} {
		if got, ok := elapsed(in); !ok || got != want {
			t.Errorf("elapsed(%q) = %s %v, want %s", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "x", "1:2:3:4", "a-01:02"} {
		if _, ok := elapsed(in); ok {
			t.Errorf("elapsed(%q) read", in)
		}
	}
}

// The process that disconnects on a copy takes as uni-switch's the providers
// the uni-switch asking has on, also those it reads from another agent's
// sign-in the copy doesn't hold (Codex's auth.json on Windows): a Claude Code
// on codex/… had an empty preview.
func TestDryRunKnowsTheAskersProviders(t *testing.T) {
	t.Cleanup(func() {
		dryProviders = nil
		disconnectDryRun = false
	})
	if isUniSwitch("elsewhere/gpt-x") {
		t.Fatal("an unknown provider's model is uni-switch's")
	}
	p := filepath.Join(t.TempDir(), dryProvidersFile)
	if err := os.WriteFile(p, []byte("elsewhere\nother"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(dryProvidersVar, p)
	_ = DryRun("no-such-agent")
	if !isUniSwitch("elsewhere/gpt-x") || isUniSwitch("third/gpt-x") || isUniSwitch("elsewhere") {
		t.Fatalf("held %v", dryProviders)
	}
}
