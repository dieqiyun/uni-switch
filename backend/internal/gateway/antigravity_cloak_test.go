package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// hermesOldSystem is Hermes Agent's earlier default persona, the SOUL.md it
// seeded until it replaced it, which a SOUL.md the user edited keeps.
const hermesOldSystem = "You are Hermes Agent, an intelligent AI assistant created by Nous Research. You are helpful, knowledgeable, and direct. You assist users with a wide range of tasks including answering questions, writing and editing code, analyzing information, creative work, and executing actions via your tools."

// googleTurnsAway is what a real Antigravity account turned away, each tried
// on one on 10-27: the line as written, in the system instruction, in any
// case. Broken up by a zero-width space after the first letter, each went
// through.
var googleTurnsAway = []string{
	"x-anthropic-billing-header:",
	"built on anthropic's claude agent sdk",
	"you are codex, an agent based on gpt-5",
	"you are codex, a coding agent based on gpt-5",
	"hermes agent, an intelligent ai assistant created by nous research",
}

func TestAntigravityCloakBreaksEveryLineItTurnsAway(t *testing.T) {
	for _, system := range []string{
		claudeCodeSystem,
		"You are a Claude agent, built on Anthropic's Claude Agent SDK.",
		codexGPT5System,
		"You are Codex, a coding agent based on GPT-5. You and the user share the same workspace.",
		hermesOldSystem,
		strings.ToUpper(hermesOldSystem),
		// twice, and after text whose lower case is longer than it is
		"İstanbul ✓ " + codexGPT5System + "\n\n" + hermesOldSystem + "\n" + codexGPT5System,
	} {
		if !antigravityTurnsAway(system) {
			t.Fatalf("%q isn't taken for what Antigravity turns away", system)
		}
		got := antigravityCloak(system)
		if antigravityTurnsAway(got) {
			t.Errorf("a line Antigravity turns away is still whole in %q", got)
		}
		if strings.ReplaceAll(got, zeroWidth, "") != system {
			t.Errorf("more than zero-width spaces changed: %q", got)
		}
		if !strings.Contains(got, zeroWidth) {
			t.Errorf("nothing broken up: %q", got)
		}
		// nothing else: each break sits after a line's first letter
		low := strings.ToLower(strings.ReplaceAll(got, zeroWidth, "|"))
		for _, line := range googleTurnsAway {
			if strings.Contains(strings.ToLower(system), line) && !strings.Contains(low, line[:1]+"|"+line[1:]) {
				t.Errorf("%q isn't broken after its first letter in %q", line, got)
			}
		}
		if antigravityCloak(got) != got {
			t.Errorf("a prompt broken up once is broken again")
		}
	}
	// what Antigravity answers is sent as written, word for word
	for _, system := range []string{
		"",
		"You are opencode, an interactive CLI tool.",
		"You are Claude Code, Anthropic's official CLI for Claude, running within the Claude Agent SDK.",
		"You are Codex, based on GPT-5. You are running as a coding agent in the Codex CLI on a user's computer.",
		"You are Hermes Agent, built by Nous Research. Be direct.",
		"You are Hermes Agent, an intelligent AI assistant created by Nous.",
	} {
		if got := antigravityCloak(system); got != system {
			t.Errorf("%q went as %q", system, got)
		}
	}
}

func TestAntigravityHermesHint(t *testing.T) {
	if antigravityHint(hermesOldSystem) != antigravityHermesHint {
		t.Error("Hermes' old persona isn't told its own hint")
	}
	if antigravityHint(codexGPT5System) != antigravityCodexHint || antigravityHint(claudeCodeSystem) != antigravityTurnedAwayHint {
		t.Error("the other prompts lost their hints")
	}
}

// fakeGoogle answers as a real Antigravity account did on 10-27: the 429
// for a system instruction holding one of googleTurnsAway's lines whole,
// and a reply otherwise. sent is the system instruction it was last sent.
func fakeGoogle(sent *string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var env struct {
			Request struct {
				SystemInstruction struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"systemInstruction"`
			} `json:"request"`
		}
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &env)
		*sent = ""
		for _, p := range env.Request.SystemInstruction.Parts {
			*sent += p.Text
		}
		low := strings.ToLower(*sent)
		for _, line := range googleTurnsAway {
			if strings.Contains(low, line) {
				w.WriteHeader(http.StatusTooManyRequests)
				io.WriteString(w, turnedAway)
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(`data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"from google"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":3}}}`))
	}
}

// hermesTurn is a Hermes Agent turn on Antigravity's model, on the Chat
// Completions API it reaches uni-switch by, with system as its system prompt.
func hermesTurn(s *Server, system string) (int, string) {
	body := `{"model":"antigravity/gemini-3-flash","stream":false,"messages":[{"role":"system","content":` + quote(system) + `},{"role":"user","content":"hi"}]}`
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	r.Header.Set("User-Agent", "hermes-agent/0.12")
	s.Handler().ServeHTTP(rec, r)
	return rec.Code, rec.Body.String()
}

func codexAGPost(s *Server, body string) (int, string) {
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body)))
	return rec.Code, rec.Body.String()
}

// Only an Antigravity account's system instruction is broken up: Gemini
// CLI's own sign-in, on the same Code Assist envelope, and every other
// provider are sent the prompt as the agent wrote it.
func TestOnlyAntigravityIsSentThePromptBrokenUp(t *testing.T) {
	r := &Request{System: hermesOldSystem + "\n" + codexGPT5System, Messages: []Message{{Role: "user", Parts: []Part{{Kind: Text, Text: "hi"}}}}}
	gemini := provider.Provider{ID: "gemini-cli", Account: &provider.Account{Agent: "gemini"}}
	if b := string(codeAssistBody(gemini, r, "gemini-3-flash", nil)); !strings.Contains(b, "You are Hermes Agent, an intelligent") || strings.Contains(b, `​`) || strings.Contains(b, zeroWidth) {
		t.Fatalf("Gemini CLI's account was sent %s", b)
	}
	ag := provider.Provider{ID: "antigravity", Account: &provider.Account{Agent: "antigravity"}}
	if b := string(codeAssistBody(ag, r, "gemini-3-flash", nil)); strings.Contains(b, "You are Hermes Agent, an intelligent") {
		t.Fatalf("Antigravity's account was sent %s", b)
	}
	if r.System != hermesOldSystem+"\n"+codexGPT5System {
		t.Fatalf("the caller's request was changed: %q", r.System)
	}
	gem, err := buildGemini(r, "m")
	if err != nil {
		t.Fatal(err)
	}
	for name, b := range map[string][]byte{
		"chat":      buildChat(r, "m", "", false),
		"anthropic": buildAnthropic(r, "m"),
		"responses": buildResponses(r, "m", "", false),
		"gemini":    gem,
	} {
		if !strings.Contains(string(b), "You are Hermes Agent, an intelligent") || strings.Contains(string(b), zeroWidth) || strings.Contains(string(b), `\u200b`) {
			t.Errorf("%s was sent the prompt broken up: %s", name, b)
		}
	}
}
