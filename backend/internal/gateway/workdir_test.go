package gateway

import (
	"os"
	"path/filepath"

	"strings"
	"testing"
)

// The fixtures are requests the agents sent a fake upstream, run in a
// sandbox home from a folder named "a&b <c> 中文" (Claude Code 2.1.296,
// Codex 0.162.0) or "My Proj" (Pi 1.1.0); the sandbox's path is replaced
// with /Users/me/work.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "workdir", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Each agent's folder is read from its own request, entities and all.
func TestWorkDirOfEachAgent(t *testing.T) {
	for _, tc := range []struct {
		file  string
		parse func([]byte) (*Request, error)
		want  string
	}{
		{"claude-code-2.1.296.json", parseAnthropic, "/Users/me/work/proj/a&b <c> 中文"},
		{"codex-0.162.0.json", parseResponses, "/Users/me/work/proj/a&b <c> 中文"},
		{"pi-1.1.0.json", parseChat, "/Users/me/work/proj/My Proj"},
	} {
		req, err := tc.parse(fixture(t, tc.file))
		if err != nil {
			t.Fatal(tc.file, err)
		}
		if got := workDir(req); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.file, got, tc.want)
		}
	}
}

// OpenCode 2.0.20's environment, as its source writes it (its run waits on
// a background service a sandbox doesn't start): in the system prompt,
// and when it changes, after.
func TestWorkDirOpenCode(t *testing.T) {
	env := func(dir string) string {
		return strings.Join([]string{"<env>", "  Working directory: " + dir, "  Workspace root folder: /Users/me", "  Is directory a git repo: yes", "  Platform: darwin",
			"  Prefer /tmp/x over generic system temporary directories such as /tmp; it is pre-created and approved for external access.", "</env>"}, "\n")
	}
	req := &Request{System: "You are opencode.\nHere is some useful information about the environment you are running in:\n" + env("/Users/me/acme"),
		Messages: []Message{{Role: "user", Parts: []Part{{Kind: Text, Text: "hi"}}}}}
	if got := workDir(req); got != "/Users/me/acme" {
		t.Errorf("%q", got)
	}
	req.Messages = append(req.Messages, Message{Role: "assistant", Parts: []Part{{Kind: Text, Text: "ok"}}},
		Message{Role: "user", Parts: []Part{{Kind: Text, Text: "The environment you are running in is now:\n" + env("/Users/me/oss")}}})
	if got := workDir(req); got != "/Users/me/oss" {
		t.Errorf("changed: %q", got)
	}
}

// Claude Code moved to another folder says so after, with the one it was
// in (its source: "Primary working directory: /y (was /x)"); the last
// said goes. What the model or a tool said, or a "Working directory:" out
// of OpenCode's <env>, isn't the agent's folder; nothing said is unknown.
func TestWorkDirLastSaidAndOnlyByTheAgent(t *testing.T) {
	req, err := parseAnthropic(fixture(t, "claude-code-2.1.296.json"))
	if err != nil {
		t.Fatal(err)
	}
	req.Messages = append(req.Messages,
		Message{Role: "assistant", Parts: []Part{{Kind: Text, Text: " - Primary working directory: /from/the/model"}}},
		Message{Role: "user", Parts: []Part{{Kind: ToolResult, Text: "<cwd>/from/a/tool</cwd>"}, {Kind: Text, Text: "<system-reminder>\nPrimary working directory: /Users/me/next (was /Users/me/work/proj/a&b &lt;c&gt; 中文)\n</system-reminder>"}}},
	)
	if got := workDir(req); got != "/Users/me/next" {
		t.Errorf("%q", got)
	}
	for _, text := range []string{"hi", "Working directory: /x", "the <cwd></cwd> tag"} {
		if got := workDir(&Request{System: "You are an agent.", Messages: []Message{{Role: "user", Parts: []Part{{Kind: Text, Text: text}}}}}); got != "" {
			t.Errorf("%q: %q", text, got)
		}
	}
}
