package gui

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// Setting Desktop's subagent model on the Agents page says nothing of
// restarting Desktop, which it needn't, unless the pick connected Desktop;
// a tier, which Desktop reads at start-up, still says it.
func TestDesktopSubagentModelNotice(t *testing.T) {
	testenv.SetHome(t, t.TempDir())
	// uni-switch's own files (the providers, the settings, Desktop's tiers)
	// live in the XDG config folder, which SetHome doesn't move; a run
	// that leaves the tier pick there wires Desktop for the next run
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	a, err := agent.Find("claude-desktop")
	if err != nil {
		t.Fatal(err)
	}
	call := func(field, value string) string {
		t.Helper()
		w := httptest.NewRecorder()
		Handler(nil, nil).ServeHTTP(w, httptest.NewRequest("POST", "/api/set", strings.NewReader(`{"agent":"claude-desktop","field":"`+field+`","value":"`+value+`"}`)))
		var out struct{ Notice string }
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 {
			t.Fatalf("set %s: %d %s (%v)", field, w.Code, w.Body.String(), err)
		}
		return out.Notice
	}
	if notice := call("subagent_effort", "high"); notice != "" || a.Wired() {
		t.Fatalf("saving an unwired preference showed notice %q or connected Desktop", notice)
	}
	if notice := call("subagent_model", "fake/m1"); notice != a.Notice() || notice == "" {
		t.Fatalf("connecting Live pick notice %q, want %q", notice, a.Notice())
	}
	if !a.Wired() {
		t.Fatal("Live pick did not connect Desktop")
	}
	if notice := call("subagent_model", ""); notice != "" {
		t.Fatalf("wired Live pick notice %q, want none", notice)
	}
	if notice := call("subagent_effort", "high"); notice != "" || provider.DesktopSubagentEffort() != "high" {
		t.Fatalf("live effort notice %q, saved %q", notice, provider.DesktopSubagentEffort())
	}
	if notice := call("subagent_effort", ""); notice != "" || provider.DesktopSubagentEffort() != "" {
		t.Fatalf("clear effort notice %q, saved %q", notice, provider.DesktopSubagentEffort())
	}
	if notice := call("sonnet", "fake/m1"); notice != a.Notice() || notice == "" {
		t.Fatalf("tier notice %q, want %q", notice, a.Notice())
	}
}
