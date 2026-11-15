package agent

import (
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// Pi's google-generative-ai resolves each level through thinkingLevelMap to
// minimal, low, medium or high and throws on any other name before it sends
// (pi-ai's resolveGoogleThinkingLevel), so a Gemini model's level by another
// name is hidden there, while the same levels on Chat stay as they were.
func TestPiGoogleThinkingLevelsAreGeminis(t *testing.T) {
	m := catalog.Model{ID: "gem/gemini-3-pro", Name: "Gemini 3 Pro", APIs: []string{"gemini"}, Efforts: []string{"none", "low", "high", "xhigh", "max"}}
	e := piModelJSON(m, "http://127.0.0.1:1", true)
	if e["api"] != "google-generative-ai" {
		t.Fatalf("api: %v", e["api"])
	}
	levels := e["thinkingLevelMap"].(map[string]any)
	for l, want := range map[string]any{"off": "none", "minimal": nil, "low": "low", "medium": nil, "high": "high", "xhigh": nil, "max": nil} {
		if levels[l] != want {
			t.Errorf("%s: %v, want %v", l, levels[l], want)
		}
	}
	if got := piSupported(e); !slices.Equal(got, []string{"off", "low", "high"}) {
		t.Errorf("Pi offers %v", got)
	}
	chat := piModelJSON(catalog.Model{ID: m.ID, Name: m.Name, Efforts: m.Efforts}, "http://127.0.0.1:1", true)
	if l := chat["thinkingLevelMap"].(map[string]any); l["xhigh"] != "xhigh" || l["max"] != "max" {
		t.Errorf("a Chat model lost its levels: %v", l)
	}
}

// Raft runs Pi's SDK on Pi's own agent folder and hands each request a
// fetch of its own, which pi-ai's Google adapter refuses before sending
// ("Custom fetch is not supported by the Google Generative AI adapter",
// yunfei9958 on X), so a Gemini model in Pi's models.json stays on Chat,
// and Pi offers it the levels Chat has; OmO and Prime Agent, whose
// folders are their own, keep Gemini's API.
func TestPiFolderKeepsGeminiOnChatForRaft(t *testing.T) {
	m := catalog.Model{ID: "gem/gemini-3.8-flash", Name: "Gemini 3.8 Flash", APIs: []string{"gemini"}, Efforts: []string{"low", "high", "xhigh"}}
	const gw = "http://127.0.0.1:1"
	for id, want := range map[string]any{"pi": nil, "stepcode": nil, "omo": "google-generative-ai", "prime-agent": "google-generative-ai"} {
		e := piModelJSON(piAsked(id, m), gw, true)
		if e["api"] != want {
			t.Errorf("%s: api %v, want %v", id, e["api"], want)
		}
		if want == nil && (e["baseUrl"] != nil || e["thinkingLevelMap"].(map[string]any)["xhigh"] != "xhigh") {
			t.Errorf("%s: not Chat's entry: %v", id, e)
		}
	}
	if !slices.Equal(m.APIs, []string{"gemini"}) {
		t.Errorf("the catalog's model changed: %v", m.APIs)
	}
}
