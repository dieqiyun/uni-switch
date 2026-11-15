package library

import (
	"path/filepath"
	"slices"
	"testing"
)

// A server turned off goes from the agents that run it only once no agent's
// config runs that command: another agent's entry, or the user's own under
// another name, keeps it (#3885).
func TestDroppedServerEndedOnceNoAgentRunsIt(t *testing.T) {
	h := sandbox(t)
	write(t, filepath.Join(h, ".codex", "config.toml"), "")
	write(t, filepath.Join(h, ".config", "opencode", "opencode.json"), "{}")
	var dropped []*Server
	was := ending
	ending = func(d []*Server) { dropped = append(dropped, d...) }
	t.Cleanup(func() { ending = was })
	names := func(ss []*Server) []string {
		var out []string
		for _, s := range ss {
			out = append(out, s.Name+": "+s.Command+" "+filepath.Join(s.Args...))
		}
		return out
	}

	ok(t)(SaveServer("", Server{Name: "tavily", Transport: "stdio", Command: "npx", Args: []string{"-y", "tavily-mcp"}, Agents: []string{"codex", "opencode"}}))
	if len(dropped) != 0 {
		t.Fatalf("dropped on adding: %v", names(dropped))
	}
	ok(t)(ServerAgents("tavily", []string{"opencode"}))
	if got := names(dropped); !slices.Equal(got, []string{"tavily: npx " + filepath.Join("-y", "tavily-mcp")}) {
		t.Fatalf("dropped from codex: %v", got)
	}
	if got := unused(dropped, Targets()); len(got) != 0 {
		t.Errorf("OpenCode runs it still, but it is ended: %v", names(got))
	}

	// the user's own entry in Claude Code, by another name, runs the same
	write(t, filepath.Join(h, ".claude.json"), `{"mcpServers": {"web": {"command": "npx", "args": ["-y", "tavily-mcp"]}}}`)
	dropped = nil
	ok(t)(ServerAgents("tavily", nil))
	if len(dropped) != 1 {
		t.Fatalf("dropped from opencode: %v", names(dropped))
	}
	if got := unused(dropped, Targets()); len(got) != 0 {
		t.Errorf("Claude Code's own entry runs it, but it is ended: %v", names(got))
	}
	write(t, filepath.Join(h, ".claude.json"), `{}`)
	if got := unused(dropped, Targets()); len(got) != 1 {
		t.Errorf("no agent runs it, but it isn't ended: %v", names(got))
	}

	// taken out of the library while on an agent
	ok(t)(ServerAgents("tavily", []string{"codex"}))
	dropped = nil
	ok(t)(RemoveServer("tavily"))
	if got := unused(dropped, Targets()); len(got) != 1 {
		t.Errorf("removed: %v, ended %v", names(dropped), names(got))
	}
}

// launchedBy reads the processes a server's command line starts, from the
// lines of #3885's Windows machine and their macOS and Linux forms.
func TestLaunchedBy(t *testing.T) {
	tavily := launchedBy(&Server{Command: "npx", Args: []string{"-y", "tavily-mcp"}})
	for line, want := range map[string]bool{
		`C:\WINDOWS\system32\cmd.exe /d /s /c "npx -y tavily-mcp"`:                                                   true,
		`"C:\Program Files\nodejs\node.exe" "C:\Program Files\nodejs\node_modules\npm\bin\npx-cli.js" -y tavily-mcp`: true,
		`node /usr/local/lib/node_modules/npm/bin/npx-cli.js -y tavily-mcp`:                                          true,
		`npm exec tavily-mcp`: false, // what npx runs is under the npx it is ended with
		`node C:\Users\u\AppData\Local\npm-cache\_npx\1\node_modules\tavily-mcp\build\index.js`: false,
		`cmd.exe /d /s /c "npx -y tavily-mcp-two"`:                                              false,
		`bunx -y tavily-mcp`:      false,
		`npx -y other tavily-mcp`: false,
		`tavily-mcp`:              false,
	} {
		if got := tavily(line); got != want {
			t.Errorf("%s: %v, want %v", line, got, want)
		}
	}
	bin := launchedBy(&Server{Command: `C:\tools\firecrawl-mcp.cmd`})
	if !bin(`C:\WINDOWS\system32\cmd.exe /d /s /c "C:\tools\firecrawl-mcp.cmd"`) || bin(`firecrawl-mcp-x`) {
		t.Error("a server with no arguments is told by its program")
	}
	// an interpreter with nothing after it is anything it runs
	for _, s := range []*Server{{Command: "node"}, {Command: "npx"}, {URL: "https://x", Command: "npx", Args: []string{"x"}}, {}} {
		if launchKey(s) != "" || launchedBy(s)(`node index.js`) || launchedBy(s)(`node`) {
			t.Errorf("%+v picks", s)
		}
	}
}
