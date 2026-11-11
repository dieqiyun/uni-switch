package gateway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// codexGPT5System is the start of Codex's own prompt for gpt-5.6-sol, as
// Codex 0.162.1 sends it: the first developer message of the thread.
const codexGPT5System = "You are Codex, an agent based on GPT-5. You and the user share one workspace, and your job is to collaborate with them until their goal is genuinely handled."

// Antigravity answers Codex's GPT-5 identity line in the system instruction
// with the same 429 it gives Claude Code's prompt, in any case; the lines
// it answers were each tried on a real Antigravity account (10-27).
func TestAntigravityTurnsAwayCodexsGPT5Prompt(t *testing.T) {
	for _, system := range []string{
		codexGPT5System,
		"You are Codex, a coding agent based on GPT-5. You and the user share the same workspace.",
		"you are codex, an agent based on gpt-5.",
		"Some instructions first.\n" + codexGPT5System,
	} {
		if !antigravityTurnsAway(system) {
			t.Errorf("%q isn't taken for what Antigravity turns away", system)
		}
		if antigravityHint(system) != antigravityCodexHint {
			t.Errorf("%q isn't told Codex's hint", system)
		}
	}
	// what it answers: uni-switch's own Codex prompt for other models, GPT-6's,
	// the line reworded, and the identity alone
	for _, system := range []string{
		"You are Codex, based on GPT-5. You are running as a coding agent in the Codex CLI on a user's computer.",
		"You are Codex, an agent based on GPT-6.",
		"You are Codex, an AI agent based on GPT-5.",
		"You are Codex, an agent built on GPT-5.",
		"You are Codex, a coding agent.",
	} {
		if antigravityTurnsAway(system) {
			t.Errorf("%q is taken for what Antigravity turns away; it answers it", system)
		}
	}
	if antigravityHint(claudeCodeSystem) != antigravityTurnedAwayHint {
		t.Error("Claude Code's prompt isn't told its own hint")
	}
}

// codexAGTurn is a Codex turn on Antigravity's model itself, on the Responses
// API, with Codex's GPT-5 prompt as its first developer message. switched
// is a thread begun on a GPT model and moved to this one (/model, resume
// -m, a fork as an agent's thread): it keeps that prompt first and says
// the switch in a later developer message, after a reply. search is
// Codex's web_search tool, which it sends every turn and an Antigravity
// model is given uni-switch's search for.
func codexAGTurn(switched, search bool) string {
	input := `{"type":"message","role":"developer","content":[{"type":"input_text","text":` + quote(codexGPT5System) + `}]},` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}`
	if switched {
		input += `,{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]},` +
			`{"type":"message","role":"developer","content":[{"type":"input_text","text":"<model_switch>\nThe user was previously using a different model.\n</model_switch>"}]},` +
			`{"type":"message","role":"user","content":[{"type":"input_text","text":"go on"}]}`
	}
	tools := ""
	if search {
		tools = `,"tools":[{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{}}},{"type":"web_search"}]`
	}
	return `{"model":"antigravity/gemini-3-flash","stream":true,"input":[` + input + `]` + tools + `}`
}

func TestAntigravityCodexHintIsTheRoutingPages(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "gui", "assets", "plugins", "routing", "client.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), quote(antigravityCodexHint)) {
		t.Fatal("plugins/routing/client.js doesn't know the note the gateway adds to Antigravity's refusal of Codex's prompt")
	}
	if !strings.Contains(string(b), quote(antigravityHermesHint)) {
		t.Fatal("plugins/routing/client.js doesn't know the note the gateway adds to Antigravity's refusal of Hermes' old persona")
	}
}
