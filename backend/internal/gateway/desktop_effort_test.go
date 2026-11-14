package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// desktopPicker is Claude Desktop's tIt (index.chunk-D3OyLXgG.js, 2.7032):
// whether the model id it was given gets a thinking-effort picker. IC is
// qS of the lowercased id; HFt's keys that have effort levels, and UFt.
func desktopPicker(id string) bool {
	e := strings.ToLower(id)
	t := regexp.MustCompile(`^arn:aws[a-z-]*:bedrock:[^/]+/`).ReplaceAllString(e, "")
	t = regexp.MustCompile(`^(?:[a-z][a-z0-9-]*\.)?anthropic\.`).ReplaceAllString(t, "")
	stripped := t != e || regexp.MustCompile(`^claude-(?:[a-z]+-)?\d`).MatchString(t)
	t = regexp.MustCompile(`\[[^\]]+\]$`).ReplaceAllString(t, "")
	if stripped {
		t = regexp.MustCompile(`-v\d+(?::\d+)?$`).ReplaceAllString(t, "")
	}
	t = regexp.MustCompile(`@\d{8}$`).ReplaceAllString(t, "")
	t = regexp.MustCompile(`-\d{8}$`).ReplaceAllString(t, "")
	switch t {
	case "claude-sonnet-4-6", "claude-sonnet-5", "claude-opus-4-6", "claude-opus-4-7", "claude-opus-4-8", "claude-opus-5":
		return true
	}
	return regexp.MustCompile(`^(?:claude-)?(?:fable|mythos)(?:-|$)`).MatchString(t)
}

// Desktop's small_fast pick (_$n): an id with haiku, sonnet or opus in it
func desktopSmallFast(id string) bool {
	for _, w := range []string{"haiku", "sonnet", "opus"} {
		if strings.Contains(id, w) {
			return true
		}
	}
	return false
}

// a model with reasoning levels is listed to Claude Desktop by an id it
// offers its effort picker for (ARNO: 接入claude desktop后无法设置思考强度):
// a Claude model by uni-switch-<n>.anthropic.<model>, any other by
// mythos-uni-switch-<n>, and so a routing group whatever model leads it (else
// Desktop names it from its catalog, "Opus 5.5", not by the group's name);
// one without levels as before. Each is kept by Desktop, served again by
// uni-switch, with or without [1m], and none but a Claude model becomes
// Desktop's small_fast pick.
func TestClaudeDesktopEffortIDs(t *testing.T) {
	levels := []string{"low", "medium", "high"}
	for _, c := range []struct {
		e      provider.Entry
		want   string // the id's form
		picker bool
	}{
		{provider.Entry{ID: "zai/glm-5.3", Model: "glm-5.3", Efforts: levels}, `^mythos-uni-switch-\d{10}$`, true},
		{provider.Entry{ID: "x/claude-sonnet-5-thinking", Model: "claude-sonnet-5-thinking", Efforts: levels}, `^mythos-uni-switch-\d{10}$`, true},
		{provider.Entry{ID: "or/anthropic/claude-opus-4-8", Model: "anthropic/claude-opus-4-8", Efforts: levels}, `^uni-switch-\d{10}\.anthropic\.claude-opus-4-8$`, true},
		{provider.Entry{ID: "x/Claude-Sonnet-4-6-20260101", Model: "Claude-Sonnet-4-6-20260101", Efforts: levels}, `^uni-switch-\d{10}\.anthropic\.claude-sonnet-4-6$`, true},
		{provider.Entry{ID: "x/claude-haiku-4-5", Model: "claude-haiku-4-5", Efforts: levels}, `^uni-switch-\d{10}\.anthropic\.claude-haiku-4-5$`, false},
		// Copilot, and the relays that follow it, write a Claude version with a
		// dot; Desktop and Claude Code read a Claude model by its dashed id
		{provider.Entry{ID: "copilot/claude-opus-4.6", Model: "claude-opus-4.6", Efforts: levels}, `^uni-switch-\d{10}\.anthropic\.claude-opus-4-6$`, true},
		{provider.Entry{ID: "copilot/claude-opus-5.5", Model: "claude-opus-5.5", Efforts: levels}, `^uni-switch-\d{10}\.anthropic\.claude-opus-5-5$`, false},
		{provider.Entry{ID: "copilot/claude-sonnet-5.5", Model: "claude-sonnet-5.5", Efforts: levels}, `^uni-switch-\d{10}\.anthropic\.claude-sonnet-5-5$`, false},
		{provider.Entry{ID: "x/claude-mythos-5.1", Model: "claude-mythos-5.1", Efforts: levels}, `^uni-switch-\d{10}\.anthropic\.claude-mythos-5-1$`, true},
		{provider.Entry{ID: "copilot/claude-haiku-4.5", Model: "claude-haiku-4.5", Efforts: []string{"none", "high"}}, `^uni-switch-\d{10}\.anthropic\.claude-haiku-4-5$`, false},
		{provider.Entry{ID: "x/Claude-Sonnet-4.5-20250929", Model: "Claude-Sonnet-4.5-20250929", Efforts: levels}, `^uni-switch-\d{10}\.anthropic\.claude-sonnet-4-5$`, false},
		{provider.Entry{ID: "x/claude-sonnet-4.5-thinking", Model: "claude-sonnet-4.5-thinking", Efforts: levels}, `^mythos-uni-switch-\d{10}$`, true},
		{provider.Entry{ID: "group/coding", Model: "claude-opus-5-5", Group: "coding", Efforts: levels}, `^mythos-uni-switch-\d{10}$`, true},
		{provider.Entry{ID: "zai/glm-4.5-air", Model: "glm-4.5-air"}, `^anthropic/uni-switch-\d{10}$`, false},
		{provider.Entry{ID: "x/claude-opus-4-8", Model: "claude-opus-4-8"}, `^x/claude-opus-4-8$`, false},
	} {
		id := claudeLooking(c.e)
		if !regexp.MustCompile(c.want).MatchString(id) {
			t.Errorf("%s listed as %s, want %s", c.e.ID, id, c.want)
		}
		if !desktopAccepts(id) {
			t.Errorf("%s: Desktop would drop %s", c.e.ID, id)
		}
		if desktopPicker(id) != c.picker {
			t.Errorf("%s as %s: picker %v", c.e.ID, id, !c.picker)
		}
		if strings.HasPrefix(id, desktopEffortAlias) && desktopSmallFast(id) {
			t.Errorf("%s: %s would be Desktop's small_fast model", c.e.ID, id)
		}
	}
	if desktopPicker("x/claude-opus-4-8") || desktopPicker(aliasFor("zai/glm-5.3")) {
		t.Fatal("the ids listed before had a picker")
	}

	setup(t, provider.Anthropic, &fake{})
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Key: "k", Anthropic: "http://127.0.0.1:1",
		Models: []string{"m1", "claude-opus-4-8", "claude-opus-5.5", "m2"}}); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"fake/m1", "fake/claude-opus-4-8", "fake/claude-opus-5.5"} {
		if err := provider.SetModelEfforts(ref, []string{"low", "high"}); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("x-api-key", Token+"-claude-desktop")
	// a build that has the mythos word (#2426); one without is listed the
	// plain alias instead (TestClaudeDesktopOldBuildModels)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Claude/2.7032.0 Chrome/140.0.0.0 Electron/37.2.0 Safari/537.36")
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, req)
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, m := range list.Data {
		got[DesktopCatalogID(m.ID)] = m.ID
	}
	for id, want := range map[string]string{
		"fake/m1":              desktopEffortAlias + aliasNumber("fake/m1"),
		"fake/claude-opus-4-8": "uni-switch-" + aliasNumber("fake/claude-opus-4-8") + ".anthropic.claude-opus-4-8",
		"fake/claude-opus-5.5": "uni-switch-" + aliasNumber("fake/claude-opus-5.5") + ".anthropic.claude-opus-5-5",
		"fake/m2":              aliasFor("fake/m2"),
	} {
		if got[id] != want {
			t.Errorf("%s listed as %q, want %q (%s)", id, got[id], want, rec.Body)
		}
		for _, asked := range []string{want, want + "[1m]"} {
			if real, ok := aliased(asked); !ok || real != id {
				t.Errorf("aliased(%s) = %q %v", asked, real, ok)
			}
		}
	}
	for _, bad := range []string{"mythos-uni-switch-0000000000", "uni-switch-0000000000.anthropic.claude-opus-4-8", "uni-switch-12.anthropic.claude-opus-4-8", "mythos-5"} {
		if _, ok := aliased(bad); ok {
			t.Errorf("aliased(%s) resolved", bad)
		}
	}
}

// the effort picked in Claude Desktop reaches the model, fitted to its
// levels: Desktop's Claude Code sends thinking plus output_config.effort
func TestClaudeDesktopEffortReachesModel(t *testing.T) {
	f := &fake{reply: sse(
		`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"m1","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}`,
		`event: message_stop`+"\n"+`data: {"type":"message_stop"}`)}
	setup(t, provider.Anthropic, f)
	if err := provider.SetModelEfforts("fake/m1", []string{"low", "high"}); err != nil {
		t.Fatal(err)
	}
	alias := desktopEffortAlias + aliasNumber("fake/m1")
	for _, c := range []struct{ asked, sent string }{
		{"low", "low"}, {"high", "high"}, {"max", "high"}, {"xhigh", "high"},
	} {
		body := `{"model":"` + alias + `","max_tokens":32000,"stream":true,"thinking":{"type":"adaptive"},"output_config":{"effort":"` + c.asked + `"},` +
			`"tools":[{"name":"Bash","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"hi"}]}`
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
		req.Header.Set("x-api-key", Token+"-claude-desktop")
		rec := httptest.NewRecorder()
		New().Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", c.asked, rec.Code, rec.Body)
		}
		var sent struct {
			Model        string `json:"model"`
			OutputConfig struct {
				Effort string `json:"effort"`
			} `json:"output_config"`
		}
		if err := json.Unmarshal(f.got, &sent); err != nil {
			t.Fatal(err)
		}
		if sent.Model != "m1" || sent.OutputConfig.Effort != c.sent {
			t.Errorf("%s: sent %s at %q, want m1 at %q", c.asked, sent.Model, sent.OutputConfig.Effort, c.sent)
		}
	}
}

// a Claude model a provider spells with a dot is listed to Desktop by its
// dashed id, so Claude Code knows it for the Claude model it is, and a turn
// on that id reaches the provider's own spelling (Copilot's claude-opus-5.5
// listed as mythos-uni-switch-<n> ran as a model Claude Code doesn't know: 32000
// output tokens, effort high, and claude-haiku-4.5 asked for an effort Copilot
// answers with 400)
func TestClaudeDesktopDottedClaudeReachesItsModel(t *testing.T) {
	f := &fake{reply: sse(
		`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"claude-opus-5.5","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}`,
		`event: message_stop`+"\n"+`data: {"type":"message_stop"}`)}
	up := setup(t, provider.Anthropic, f)
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Key: "k", Anthropic: up.URL,
		Models: []string{"claude-opus-5.5"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelEfforts("fake/claude-opus-5.5", []string{"low", "medium", "high"}); err != nil {
		t.Fatal(err)
	}
	listed := claudeLooking(provider.Entry{ID: "fake/claude-opus-5.5", Model: "claude-opus-5.5", Efforts: []string{"low", "medium", "high"}})
	if want := "uni-switch-" + aliasNumber("fake/claude-opus-5.5") + ".anthropic.claude-opus-5-5"; listed != want {
		t.Fatalf("listed as %q, want %q", listed, want)
	}
	for _, asked := range []string{listed, listed + "[1m]"} {
		body := `{"model":"` + asked + `","max_tokens":32000,"stream":true,"thinking":{"type":"adaptive"},"output_config":{"effort":"high"},` +
			`"tools":[{"name":"Bash","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"hi"}]}`
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
		req.Header.Set("x-api-key", Token+"-claude-desktop")
		rec := httptest.NewRecorder()
		New().Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", asked, rec.Code, rec.Body)
		}
		var sent struct {
			Model        string `json:"model"`
			OutputConfig struct {
				Effort string `json:"effort"`
			} `json:"output_config"`
		}
		if err := json.Unmarshal(f.got, &sent); err != nil {
			t.Fatal(err)
		}
		if sent.Model != "claude-opus-5.5" || sent.OutputConfig.Effort != "high" {
			t.Errorf("%s: sent %s at %q, want claude-opus-5.5 at high", asked, sent.Model, sent.OutputConfig.Effort)
		}
	}
}
