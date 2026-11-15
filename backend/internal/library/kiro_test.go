package library

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Kiro CLI reads global skills from ~/.kiro/skills, its user-wide MCP
// servers from ~/.kiro/settings/mcp.json and ~/.kiro/steering/AGENTS.md of
// its global steering (kiro.dev/docs/skills, /docs/mcp/configuration,
// /docs/steering), but the library gave it none of the three: its targets
// are agent.All and apps, which held Claude Desktop alone, and Kiro CLI is
// no agent uni-switch wires a model into (#4769, ump45nose).
func TestKiroCLITarget(t *testing.T) {
	h := sandbox(t)
	holdCLI(t, h, "kiro-cli") // found by the kiro-cli program alone
	tg := targetByID("kiro-cli")
	if tg == nil {
		t.Fatalf("kiro-cli is not a target: %v", ids(Targets()))
	}
	if tg.Instructions != filepath.Join(h, ".kiro", "steering", "AGENTS.md") {
		t.Errorf("instructions: %q", tg.Instructions)
	}
	if tg.Skills != filepath.Join(h, ".kiro", "skills") {
		t.Errorf("skills: %q", tg.Skills)
	}
	if tg.MCP == nil || tg.MCP.Path != filepath.Join(h, ".kiro", "settings", "mcp.json") {
		t.Errorf("mcp: %+v", tg.MCP)
	}
	for _, kind := range []string{"instructions", "mcp", "skills"} {
		if id, err := Takes("kiro-cli", kind); id != "kiro-cli" || err != nil {
			t.Errorf("Takes(kiro-cli, %s): %q %v", kind, id, err)
		}
	}

	// the three are given as the page ticks them
	shared := "Be brief."
	ok(t)(SaveInstructions(InstructionsChange{Shared: &shared, Agents: []string{"kiro-cli"}}))
	if got := read(t, filepath.Join(h, ".kiro", "steering", "AGENTS.md")); !strings.Contains(got, "Be brief.") {
		t.Errorf("kiro's steering has no instructions:\n%s", got)
	}
	src := filepath.Join(h, "src/skills")
	skill(t, filepath.Join(src, "pdf"), "pdf", "Read PDFs")
	if _, err := ProbeSkills(src); err != nil {
		t.Fatal(err)
	}
	ok(t)(InstallSkills(src, []string{"pdf"}, []string{"kiro-cli"}))
	if _, err := os.Stat(filepath.Join(h, ".kiro", "skills", "pdf", "SKILL.md")); err != nil {
		t.Error("kiro has no pdf skill")
	}
}

// ~/.kiro alone is no sign of it: Kiro Crew keeps ~/.kiro/crew and the
// Kiro IDE keeps ~/.kiro too, with no kiro-cli installed (#4769). Found
// by the program alone, where provider.KiroExecutable looks.
func TestKiroCLINotByItsFolder(t *testing.T) {
	sandbox(t)
	old := provider.KiroExecutable
	provider.KiroExecutable = func() string { return "" }
	t.Cleanup(func() { provider.KiroExecutable = old })
	write(t, filepath.Join(home(), ".kiro", "crew", "config.json"), "{}")
	write(t, filepath.Join(home(), ".kiro", "settings", "mcp.json"), "{}")
	write(t, filepath.Join(home(), ".kiro", "steering", "AGENTS.md"), "mine\n")
	if tg := targetByID("kiro-cli"); tg != nil {
		t.Errorf("~/.kiro alone made kiro-cli a target: %+v", tg)
	}
}

// The mcp.json uni-switch writes is Kiro's own shape: mcpServers of
// command/args/env, or url/headers — no type on an entry, the user's
// fields kept, no SSE named, a ${NAME} passed through as the docs say
// (kiro.dev/docs/mcp/configuration) (#4769).
func TestKiroMCP(t *testing.T) {
	h := sandbox(t)
	holdCLI(t, h, "kiro-cli")
	p := filepath.Join(h, ".kiro", "settings", "mcp.json")
	write(t, p, `{
  "mcpServers": {
    "mine": {
      "command": "uvx",
      "args": ["mine-mcp"],
      "autoApprove": ["read"],
      "disabled": false
    }
  }
}`)
	tg := targetByID("kiro-cli")
	if tg == nil || tg.MCP == nil {
		t.Fatalf("kiro-cli has no MCP target: %+v", tg)
	}
	if tg.MCP.Format != fmtKiro {
		t.Errorf("format: %v", tg.MCP.Format)
	}

	agents := []string{"kiro-cli"}
	stdio := Server{Name: "fs", Transport: "stdio", Command: "npx", Args: []string{"-y", "@mcp/fs"}, Env: map[string]string{"K": "V"}, Agents: agents}
	web := Server{Name: "web", Transport: "http", URL: "https://example.com/mcp", Headers: map[string]string{"Authorization": "Bearer ${TOKEN}"}, Agents: agents}
	for _, s := range []Server{stdio, web} {
		ok(t)(SaveServer("", s))
	}

	var f struct {
		Servers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(read(t, p)), &f); err != nil {
		t.Fatalf("%v:\n%s", err, read(t, p))
	}
	for name, s := range f.Servers {
		for _, k := range []string{"type", "transport"} {
			if _, ok := s[k]; ok {
				t.Errorf("%s has %q, which Kiro's entries never name:\n%s", name, k, read(t, p))
			}
		}
	}
	if fs := f.Servers["fs"]; fs["command"] != "npx" {
		t.Errorf("fs: %v", fs)
	} else if e, _ := fs["env"].(map[string]any); e["K"] != "V" {
		t.Errorf("fs env: %v", fs)
	}
	if w := f.Servers["web"]; w["url"] != "https://example.com/mcp" {
		t.Errorf("web: %v", w)
	} else if hd, _ := w["headers"].(map[string]any); hd["Authorization"] != "Bearer ${TOKEN}" {
		t.Errorf("web headers, ${NAME} passed through: %v", w)
	}
	if m := f.Servers["mine"]; m["command"] != "uvx" || m["autoApprove"] == nil || m["disabled"] != false {
		t.Errorf("the user's fields kept: %v", m)
	}

	// read back, and left as they are by a sync that changes nothing
	read1, err := tg.MCP.read()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []Server{stdio, web} {
		if s := read1[want.Name]; s == nil || !s.same(&want) {
			t.Errorf("%s read back as %+v", want.Name, s)
		}
	}
	before := read(t, p)
	ok(t)(Sync())
	if read(t, p) != before {
		t.Errorf("a sync rewrote servers that are as the library has them:\n%s", read(t, p))
	}

	// no SSE named: a url is streamable HTTP, as the docs give no way to
	// say SSE (kiro.dev/docs/mcp/configuration)
	if err := tg.MCP.supports(&Server{Transport: "sse"}); !errors.Is(err, errNoSSE) {
		t.Errorf("sse: %v", err)
	}
	if err := tg.MCP.supports(&Server{Transport: "http"}); err != nil {
		t.Errorf("http: %v", err)
	}
	u, _ := tg.MCP.decode("by-url", map[string]any{"url": "https://example.com/sse", "transport": "sse"})
	if u == nil || u.Transport != "http" {
		t.Errorf("a url reads as streamable HTTP, got %+v", u)
	}

	for _, s := range []string{"fs", "web"} {
		ok(t)(RemoveServer(s))
	}
	after, _ := tg.MCP.read()
	if len(after) != 1 || after["mine"] == nil || after["mine"].Command != "uvx" {
		t.Errorf("after: %+v\n%s", after, read(t, p))
	}
	got := read(t, p)
	for _, want := range []string{"\"autoApprove\"", "\"disabled\""} {
		if !strings.Contains(got, want) {
			t.Errorf("lost the user's %s:\n%s", want, got)
		}
	}
}

// Kiro CLI uninstalled after the library gave it a server is listed as
// gone, on that server's row, for it to be unticked and taken out of
// ~/.kiro (the same as any agent, TestGoneAgentListedToTakeOut). Found by
// kiro-cli alone, it was in apps only while installed, so held never saw
// it, and uni-switch's entry stayed in Kiro's mcp.json with nothing on the
// page to take it out (#4769). Nor is an uninstalled one told it takes
// anything.
func TestKiroCLIUninstalledHeld(t *testing.T) {
	h := sandbox(t)
	holdCLI(t, h, "kiro-cli")
	p := filepath.Join(h, ".kiro", "settings", "mcp.json")
	ok(t)(SaveServer("", Server{Name: "fs", Transport: "stdio", Command: "npx", Args: []string{"-y", "@mcp/fs"}, Agents: []string{"claude", "kiro-cli"}}))
	if !strings.Contains(read(t, p), "@mcp/fs") {
		t.Fatalf("kiro-cli wasn't given the server:\n%s", read(t, p))
	}

	os.Remove(filepath.Join(h, "bin", "kiro-cli"))
	os.Remove(filepath.Join(h, "bin", "kiro-cli.exe"))
	if tg := targetByID("kiro-cli"); tg != nil {
		t.Fatalf("kiro-cli is still a target uninstalled: %+v", tg)
	}
	for _, kind := range []string{"instructions", "mcp", "skills"} {
		if id, err := Takes("kiro-cli", kind); err == nil || !strings.Contains(err.Error(), "isn't installed") {
			t.Errorf("Takes(kiro-cli, %s) uninstalled: %q %v", kind, id, err)
		}
	}
	gone := func() *AgentView {
		t.Helper()
		v, err := Read(nil)
		if err != nil {
			t.Fatal(err)
		}
		for i, a := range v.Agents {
			if a.ID == "kiro-cli" {
				return &v.Agents[i]
			}
		}
		return nil
	}
	if a := gone(); a == nil || !a.Gone || a.MCP == "" {
		t.Fatalf("kiro-cli, uninstalled holding a server, isn't listed as gone: %+v", a)
	}
	ok(t)(ServerAgents("fs", []string{"claude"}))
	if strings.Contains(read(t, p), "@mcp/fs") {
		t.Errorf("the server unticked is still in the uninstalled kiro-cli's mcp.json:\n%s", read(t, p))
	}
	if a := gone(); a != nil {
		t.Errorf("kiro-cli, holding nothing, is still listed: %+v", a)
	}
}
