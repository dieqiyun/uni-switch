package gateway

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"github.com/dieqiyun/uni-switch/backend/internal/wslrun"
)

// leanRun is what a run of a leanHarness was started with and told (its
// log) and the system prompt file it was given, read while it ran, as the
// file goes with the run.
type leanRun struct {
	log    string
	system string
	hasSys bool
}

// newLeanHarness is a resumeHarness whose Claude Code also keeps the
// system prompt file it is given.
func newLeanHarness(t *testing.T) (*resumeHarness, func() []leanRun) {
	t.Helper()
	sys := t.TempDir()
	h := newScriptHarness(t, "ok", `prev=
for a in "$@"; do
  if [ "$prev" = "--system-prompt-file" ]; then cp "$a" `+sys+`/$$; fi
  prev=$a
done`)
	return h, func() []leanRun {
		var runs []leanRun
		for pid, log := range h.runs() {
			r := leanRun{log: log}
			if b, err := os.ReadFile(filepath.Join(sys, pid)); err == nil {
				r.system, r.hasSys = string(b), true
			}
			runs = append(runs, r)
		}
		return runs
	}
}

// told is the run of runs that was told say.
func told(t *testing.T, runs []leanRun, say string) leanRun {
	t.Helper()
	for _, r := range runs {
		if i := strings.Index(r.log, "\ntold "); i >= 0 && strings.Contains(r.log[i:], say) {
			return r
		}
	}
	t.Fatalf("no run was told %q: %+v", say, runs)
	return leanRun{}
}

func leanSetting(t *testing.T, on bool) {
	t.Helper()
	if err := settings.Save(settings.Settings{ClaudeLeanPrompt: on}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = settings.Save(settings.Settings{}) })
}

func (h *resumeHarness) post(body string, header ...string) {
	h.t.Helper()
	r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	if h.from != "" {
		r.RemoteAddr = h.from
	}
	for i := 0; i+1 < len(header); i += 2 {
		r.Header.Set(header[i], header[i+1])
	}
	var u Usage
	if code, msg := h.s.serveClaudeSubscription(httptest.NewRecorder(), r, provider.Anthropic, h.p, "claude-sonnet-5", []byte(body), &u); code != 200 {
		h.t.Fatalf("%d %s", code, msg)
	}
}

// With Settings' lean prompt on, a run is started with the caller's own
// instructions as its system prompt, in place of Claude Code's: given in a
// file, the client Claude Code's billing line left out and its tool choice
// said, and its first message holds the conversation alone. Off, the
// instructions are in the first message, as before.
func TestClaudeLeanPromptIsTheRunsSystemPrompt(t *testing.T) {
	h, runs := newLeanHarness(t)
	h.from = "127.0.0.1:51000" // a caller elsewhere is told where its tools run
	leanSetting(t, true)
	h.post(`{"model":"claude-sonnet-5","max_tokens":100,"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.301.eca; cc_entrypoint=cli; cch=00000;"},{"type":"text","text":"You are Aria, a notes assistant."}],"messages":[{"role":"user","content":"turn-one"}]}`)
	h.post(`{"model":"claude-sonnet-5","max_tokens":100,"system":"Rules.","tools":[{"name":"look","input_schema":{"type":"object"}}],"tool_choice":{"type":"any"},"messages":[{"role":"user","content":"turn-two"}]}`)

	one := told(t, runs(), "turn-one")
	if !one.hasSys || one.system != "You are Aria, a notes assistant." {
		t.Fatalf("the run's system prompt (given %t): %q\n%s", one.hasSys, one.system, one.log)
	}
	two := told(t, runs(), "turn-two")
	if two.system != "Rules.\nYou must call at least one available tool before answering." {
		t.Fatalf("the tool choice is not in the system prompt: %q", two.system)
	}
	for _, r := range []leanRun{one, two} {
		said := r.log[strings.Index(r.log, "\ntold "):]
		if strings.Contains(said, "external_system_instructions") || strings.Contains(said, "Aria") || strings.Contains(said, "Rules.") || strings.Contains(said, "must call") {
			t.Fatalf("the run was told the instructions in its message too:\n%s", r.log)
		}
	}

	leanSetting(t, false)
	h.post(`{"model":"claude-sonnet-5","max_tokens":100,"system":"You are Aria, a notes assistant.","messages":[{"role":"user","content":"turn-three"}]}`)
	if off := told(t, runs(), "turn-three"); off.hasSys || strings.Contains(off.log, "--system-prompt-file") || !strings.Contains(off.log, "external_system_instructions") {
		t.Fatalf("with the setting off, the run was given a system prompt (%t) or not told the instructions:\n%s", off.hasSys, off.log)
	}
}

// A client that places its own cache marks (#1717) keeps Claude Code's
// system prompt with the setting on: its marks on the system prompt are
// placed in the run's first message.
func TestClaudeLeanPromptNotWithClientCacheMarks(t *testing.T) {
	h, runs := newLeanHarness(t)
	leanSetting(t, true)
	h.post(`{"model":"claude-sonnet-5","max_tokens":100,"system":[{"type":"text","text":"Rules.","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"turn-one"}]}`,
		"X-Uni-Switch-Cache-Marks", "client")
	if r := told(t, runs(), "turn-one"); r.hasSys || !strings.Contains(r.log, "external_system_instructions") {
		t.Fatalf("a client cache marks request was given a system prompt (%t):\n%s", r.hasSys, r.log)
	}
}

// A run started with one system prompt goes on with no turn asked of the
// other: one started lean was never told Claude Code's prompt, and one
// started without it has the caller's instructions in its first message.
// The next turn after the setting changed starts a run anew.
func TestClaudeLeanPromptRunNotResumedAcrossTheSetting(t *testing.T) {
	h, runs := newLeanHarness(t)
	leanSetting(t, true)
	h.ask("Rules.", "turn-one")
	h.ask("Rules.", "turn-one", "turn-two")
	if got := runs(); len(got) != 1 {
		t.Fatalf("the next turn with the setting unchanged started %d runs, want it to go on in the first", len(got))
	}
	leanSetting(t, false)
	h.ask("Rules.", "turn-one", "turn-two", "turn-three")
	got := runs()
	if len(got) != 2 {
		t.Fatalf("the turn after the setting went off went on in the lean run: %d runs", len(got))
	}
	if r := told(t, got, "turn-three"); r.hasSys {
		t.Fatalf("the turn after the setting went off was told a lean run:\n%s", r.log)
	}
}

// A Claude Code in WSL reads the system prompt file under the distro's
// mount of the drive.
func TestClaudeLeanPromptFileInWSL(t *testing.T) {
	c := claudeCLI{wsl: &wslrun.Tool{Path: "/usr/bin/claude"}}
	if got := c.file(`C:\Users\me\AppData\Local\Temp\uni-switch-claude-1\system.md`); got != "/mnt/c/Users/me/AppData/Local/Temp/uni-switch-claude-1/system.md" {
		t.Fatalf("%q", got)
	}
	if got := (claudeCLI{}).file("/tmp/x/system.md"); got != "/tmp/x/system.md" {
		t.Fatalf("%q", got)
	}
}

// A lean run's system prompt says where the caller's tools run when the
// caller is elsewhere, as a run that keeps Claude Code's prompt is told
// after the caller's instructions: Claude Code gives the gateway's
// environment in the run's first message all the same, after the system
// prompt. A caller on this computer naming no folder is told nothing more.
func TestClaudeLeanPromptSaysWhereToolsRun(t *testing.T) {
	h, runs := newLeanHarness(t)
	leanSetting(t, true)
	h.from = "192.0.2.7:51000"
	h.post(`{"model":"claude-sonnet-5","max_tokens":100,"system":"Rules.","messages":[{"role":"user","content":"turn-one"}]}`)
	h.post(`{"model":"claude-sonnet-5","max_tokens":100,"system":"Rules.\n - Primary working directory: /no/such/folder/here","messages":[{"role":"user","content":"turn-two"}]}`)
	h.from = "127.0.0.1:51000"
	h.post(`{"model":"claude-sonnet-5","max_tokens":100,"system":"Rules.","messages":[{"role":"user","content":"turn-three"}]}`)

	if one := told(t, runs(), "turn-one"); one.system != "Rules."+envUnknownNote {
		t.Fatalf("a caller elsewhere naming no folder: %q", one.system)
	}
	if two := told(t, runs(), "turn-two"); !strings.HasSuffix(two.system, envElsewhereLeanNote) {
		t.Fatalf("a caller naming a folder elsewhere: %q", two.system)
	}
	if three := told(t, runs(), "turn-three"); three.system != "Rules." {
		t.Fatalf("a caller on this computer: %q", three.system)
	}
}

// A caller elsewhere whose environment is a message, as Claude Code 2.1.296
// and later send it (a {role: "system"} message after the user's first; the
// shape below is 2.1.314's, captured), is told its environment is the one
// whose working directory it names, in both modes. The run's own Claude
// Code gives its environment the same way, after the run's first message
// (2.1.316 with claude-opus-5-5, captured), so after the caller's: the
// earlier "the last one given" pointed the model at the gateway's computer
// (packinggoods on Discord: a Mac client of a Windows gateway told to use
// PowerShell). A caller naming its folder in its instructions is told the
// note it was before.
func TestClaudeCallerEnvironmentInAMessageIsNamedByItsFolder(t *testing.T) {
	const env = `# Environment\nYou have been invoked in the following environment: \n - Primary working directory: /Users/someone/proj\n - Is a git repository: false\n - Platform: darwin\n - Shell: zsh\n - OS Version: Darwin 25.2.0`
	body := func(say string) string {
		return `{"model":"claude-sonnet-5","max_tokens":100,` +
			`"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.314.cfe; cc_entrypoint=sdk-cli;"},{"type":"text","text":"You are a Claude agent, built on Anthropic's Claude Agent SDK."},{"type":"text","text":"Rules."}],` +
			`"messages":[{"role":"user","content":[{"type":"text","text":"<system-reminder>\nAttribution.\n</system-reminder>\n"},{"type":"text","text":"` + say + `"}]},` +
			`{"role":"system","content":[{"type":"text","text":"` + env + `"}]}],` +
			`"tools":[{"name":"Bash","description":"Run a command.","input_schema":{"type":"object","properties":{"command":{"type":"string"}}}}]}`
	}
	const last = "the one whose working directory is /Users/someone/proj is the one every tool runs in"
	for _, lean := range []bool{false, true} {
		h, runs := newLeanHarness(t)
		leanSetting(t, lean)
		h.from = "192.0.2.7:51000"
		h.post(body("turn-one"))
		h.post(`{"model":"claude-sonnet-5","max_tokens":100,"system":"Rules.\n - Primary working directory: /no/such/folder/here","messages":[{"role":"user","content":"turn-two"}]}`)

		one, two := told(t, runs(), "turn-one"), told(t, runs(), "turn-two")
		if lean {
			if !strings.HasSuffix(one.system, envTurnsNote("/Users/someone/proj")) || !strings.Contains(one.system, last) || strings.Contains(one.system, "the last one given") {
				t.Fatalf("lean, environment in a message: system %q", one.system)
			}
			if !strings.HasSuffix(two.system, envElsewhereLeanNote) {
				t.Fatalf("lean, environment in the instructions: system %q", two.system)
			}
			continue
		}
		i := strings.Index(one.log, "\ntold ")
		msg := one.log[i:]
		if n, at, end := strings.Count(msg, last), strings.Index(msg, last), strings.Index(msg, "/external_system_instructions"); n != 1 || at > end || strings.Contains(msg, "every tool runs in. Where an environment given before") || strings.Contains(msg, "the last one given") {
			t.Fatalf("environment in a message: told the note %d times, at %d (instructions end at %d): %s", n, at, end, msg)
		}
		if strings.Index(msg, "Platform: darwin") < strings.Index(msg, last) {
			t.Fatalf("the caller's environment is not after the note: %s", msg)
		}
		if two := two.log[strings.Index(two.log, "\ntold "):]; strings.Contains(two, last) || !strings.Contains(two, "every tool runs in. Where an environment given before") {
			t.Fatalf("environment in the instructions: %s", two)
		}
	}
}
