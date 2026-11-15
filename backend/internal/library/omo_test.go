package library

import (
	"path/filepath"
	"slices"
	"testing"
)

// OmO's engine reads its agent folder's AGENTS.md, skills and mcp.json
// (#264), the servers in the shape Pi 0.99's own has; a key its schema
// doesn't know refuses the whole file, so none of the adapters' is
// written. OMO_CODING_AGENT_DIR moves all three.
func TestOmoTarget(t *testing.T) {
	h := sandbox(t)
	d := filepath.Join(h, ".omo", "agent")
	write(t, filepath.Join(d, "settings.json"), "{}")
	holdCLI(t, h, "omo") // the folder alone is gone (#2177)
	tg := targetByID("omo")
	if tg == nil || tg.MCP == nil || tg.MCP.Path != filepath.Join(d, "mcp.json") || tg.MCP.Format != fmtPiNative ||
		tg.Instructions != filepath.Join(d, "AGENTS.md") || tg.Skills != filepath.Join(d, "skills") {
		t.Fatalf("%+v", tg)
	}
	native := tg.MCP.Path
	write(t, native, `{"settings": {"toolPrefix": "mcp"}, "mcpServers": {"theirs": {"command": "t", "lifecycle": "eager"}}}`)
	ok(t)(SaveServer("", Server{Name: "docs", Transport: "http", URL: "https://example.com/mcp",
		Headers: map[string]string{"Authorization": "Bearer ${DOCS_TOKEN}"}, Agents: []string{"omo"}}))
	ok(t)(SaveServer("", Server{Name: "fs", Transport: "stdio", Command: "npx", Args: []string{"-y", "fs"},
		Env: map[string]string{"K": "${K}"}, Agents: []string{"omo"}}))
	got := piServers(t, native)
	allowed := []string{"type", "url", "command", "args", "env", "cwd", "headers", "auth", "bearerTokenEnv", "oauth", "enabled",
		"lifecycle", "idleTimeoutMin", "requestTimeoutMs", "connectTimeoutMs", "startupTimeoutMs", "includeTools", "excludeTools",
		"directTools", "exposure", "logLevel"}
	for name, e := range got {
		for k := range e {
			if !slices.Contains(allowed, k) {
				t.Errorf("%s has %q, which OmO refuses", name, k)
			}
		}
	}
	if got["docs"]["url"] != "https://example.com/mcp" || got["fs"]["command"] != "npx" || got["theirs"]["lifecycle"] != "eager" ||
		piDoc(t, native)["settings"] == nil {
		t.Fatalf("mcp.json: %s", read(t, native))
	}
	if exists(filepath.Join(h, ".pi", "agent", "mcp.json")) || exists(filepath.Join(h, ".pi", "agent", "mcp-adapter.json")) {
		t.Error("Pi's files written")
	}
	if err := tg.MCP.supports(&Server{Transport: "sse"}); err != errNoSSE {
		t.Errorf("sse: %v", err)
	}
	ok(t)(RemoveServer("fs"))
	if piServers(t, native)["fs"] != nil {
		t.Error("fs left behind")
	}

	own := filepath.Join(h, "omo-elsewhere")
	t.Setenv("OMO_CODING_AGENT_DIR", own)
	write(t, filepath.Join(own, "settings.json"), "{}")
	if tg := targetByID("omo"); tg == nil || tg.MCP.Path != filepath.Join(own, "mcp.json") || tg.Skills != filepath.Join(own, "skills") {
		t.Fatalf("OMO_CODING_AGENT_DIR: %+v", tg)
	}
}
