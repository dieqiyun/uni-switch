package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// nancyCodex is NancyTom18's Codex (on X): signed in with ChatGPT, its own
// models listed, and a routing group named for one of them.
func nancyCodex(t *testing.T) (*Agent, func() string) {
	t.Helper()
	home, read := codexHome(t, chatGPTAuth, "model = \"gpt-5.5\"\n")
	os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"),
		[]byte(`{"models":[{"slug":"gpt-5.5","visibility":"list","priority":1},{"slug":"gpt-5.6-luna","visibility":"list","priority":2}]}`), 0o644)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"gpt-5.6-luna"}}); err != nil {
		t.Fatal(err)
	}
	return codex(home), read
}

// ownPath is how the model picker tags each of Codex's own models: "via"
// (through uni-switch) or "direct".
func ownPath(t *testing.T, cx *Agent) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, o := range cx.Fields[0].Options(cx.Values()) {
		if o.Group != "OpenAI" || o.Ref != "" {
			continue
		}
		switch {
		case o.Via && o.Direct == "":
			out[o.Value] = "via"
		case !o.Via && o.Direct != "":
			out[o.Value] = "direct"
		default:
			out[o.Value] = "both or neither"
		}
	}
	if len(out) == 0 {
		t.Fatal("no own models in the picker")
	}
	return out
}

// Connected by the switch, on a routing group, disconnected and restored,
// then switched on again: Codex comes back as the switch first connected
// it, joined, so one of its own models picked on the Agents page stays
// through uni-switch, as the picker says. Put back on the group alone, it was
// beside its sign-in and not joined: the OpenAI model picked took Codex
// off uni-switch and every row said direct (NancyTom18's first screenshot).
func TestCodexReconnectStaysJoined(t *testing.T) {
	cx, read := nancyCodex(t)
	if c, err := cx.ConnectHow(); err != nil || c.How != "joined" {
		t.Fatalf("connect: %+v %v", c, err)
	}
	if err := cx.Apply("model", "relay/gpt-5.6-luna"); err != nil {
		t.Fatal(err)
	}
	if err := cx.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); strings.Contains(cfg, "openai_base_url") {
		t.Fatalf("disconnected:\n%s", cfg)
	}
	if c, err := cx.ConnectHow(); err != nil || c.How != "again" {
		t.Fatalf("connect again: %+v %v", c, err)
	}
	if cfg := read(); !strings.Contains(cfg, `model = "relay/gpt-5.6-luna"`) || !strings.Contains(cfg, "openai_base_url") {
		t.Fatalf("connected again, not on the group:\n%s", cfg)
	}
	if !cx.Joined() {
		t.Fatal("connected again, not joined")
	}
	for m, p := range ownPath(t, cx) {
		if p != "via" {
			t.Errorf("connected again: %s says %s", m, p)
		}
	}
	if err := cx.Apply("model", "gpt-5.6-luna"); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); !strings.Contains(cfg, "openai_base_url") || !strings.Contains(cfg, `model = "gpt-5.6-luna"`) || !cx.Wired() {
		t.Fatalf("an own model picked took Codex off uni-switch, wired %v:\n%s", cx.Wired(), cfg)
	}
	for m, p := range ownPath(t, cx) {
		if p != "via" {
			t.Errorf("on its own model: %s says %s", m, p)
		}
	}
}

// Beside its sign-in on a routing group and not joined (a uni-switch model
// picked while Codex wasn't connected, or a Codex connected again by an
// older uni-switch), one of its own picked here takes uni-switch out: each says
// direct, so the picker asks before it does. It said via uni-switch, and the
// pick went straight to OpenAI (NancyTom18's second screenshot).
func TestCodexBesideOwnModelsSayDirect(t *testing.T) {
	cx, read := nancyCodex(t)
	if err := cx.Apply("model", "relay/gpt-5.6-luna"); err != nil {
		t.Fatal(err)
	}
	if !cx.Beside() || cx.Joined() || !cx.Wired() {
		t.Fatalf("not beside alone: beside %v joined %v wired %v\n%s", cx.Beside(), cx.Joined(), cx.Wired(), read())
	}
	for m, p := range ownPath(t, cx) {
		if p != "direct" {
			t.Errorf("beside: %s says %s", m, p)
		}
	}
	// and it is so
	if err := cx.Apply("model", "gpt-5.6-luna"); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); strings.Contains(cfg, "openai_base_url") || cx.Wired() {
		t.Fatalf("picked one of its own, still on uni-switch:\n%s", cfg)
	}
}
