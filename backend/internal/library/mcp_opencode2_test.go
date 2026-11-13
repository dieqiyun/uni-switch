package library

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// OpenCode 2 keeps its servers under mcp.servers (its own `opencode mcp
// add` writes them there), beside the V1 mcp.<name> entries it still
// reads. liam_8829 (Discord #feedback, 10-27) turned off every MCP server
// in uni-switch and found a "servers" node with no type and no enabled left
// in opencode.json: uni-switch read mcp.servers as nothing at all, so the
// servers in it were never found, never taken off, and uni-switch's own went
// in beside them in the other shape.
func TestOpenCode2Servers(t *testing.T) {
	h := sandbox(t)
	oc := filepath.Join(h, ".config", "opencode", "opencode.json")
	// as opencode 2.0.25's mcp add --global wrote it, with a V1 server
	// and a V2 one the user keeps for themselves
	write(t, oc, `{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "mine": {"type": "local", "command": ["node", "x.js"], "enabled": false},
    "servers": {
      "ctx7": {"type": "remote", "url": "https://mcp.context7.com/mcp"},
      "fs": {
        "type": "local",
        "command": ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
        "environment": {"A": "b"},
        "timeout": 9000
      },
      "own": {"type": "local", "command": ["uvx", "own-mcp"], "disabled": true}
    }
  }
}
`)
	l, err := load()
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, f := range foundServersOf(l, Targets()) {
		found = append(found, f.Server.Name)
	}
	if want := []string{"ctx7", "fs", "mine", "own"}; !slices.Equal(found, want) {
		t.Fatalf("found %v, want %v (mcp.servers' own, never a server called servers)", found, want)
	}
	for _, n := range []string{"ctx7", "fs"} {
		ok(t)(ImportServer(n))
	}
	ok(t)(SaveServer("", Server{Name: "gh", Transport: "http", URL: "https://api.githubcopilot.com/mcp/", Agents: []string{"opencode"}}))

	var doc struct {
		MCP map[string]json.RawMessage `json:"mcp"`
	}
	parse := func() map[string]map[string]any {
		t.Helper()
		doc.MCP = nil
		if err := json.Unmarshal([]byte(read(t, oc)), &doc); err != nil {
			t.Fatalf("opencode.json: %v\n%s", err, read(t, oc))
		}
		var servers map[string]map[string]any
		if err := json.Unmarshal(doc.MCP["servers"], &servers); err != nil {
			t.Fatalf("mcp.servers: %v\n%s", err, read(t, oc))
		}
		return servers
	}
	servers := parse()
	gh := servers["gh"]
	if gh == nil || gh["url"] != "https://api.githubcopilot.com/mcp/" {
		t.Fatalf("gh isn't written where the file keeps its servers:\n%s", read(t, oc))
	}
	if _, set := gh["enabled"]; set {
		t.Errorf("an OpenCode 2 server has no enabled (it's on unless disabled):\n%s", read(t, oc))
	}
	if _, v1 := doc.MCP["gh"]; v1 {
		t.Errorf("gh written in the V1 shape too:\n%s", read(t, oc))
	}
	if fs := servers["fs"]; fs["timeout"] != float64(9000) {
		t.Errorf("fs lost the user's timeout:\n%s", read(t, oc))
	}

	// every server off, as the page's "servers/agents-all" does
	ok(t)(EveryServerAgents([]string{"opencode"}, false))
	servers = parse()
	for _, n := range []string{"ctx7", "fs", "gh"} {
		if servers[n] != nil || doc.MCP[n] != nil {
			t.Errorf("%s is still in opencode.json after every server was turned off:\n%s", n, read(t, oc))
		}
	}
	if !strings.Contains(read(t, oc), `"own": {"type": "local", "command": ["uvx", "own-mcp"], "disabled": true}`) ||
		!strings.Contains(read(t, oc), `"mine": {"type": "local", "command": ["node", "x.js"], "enabled": false}`) {
		t.Errorf("the user's own servers changed:\n%s", read(t, oc))
	}
	// what is left is what OpenCode reads: a server under mcp is one with
	// a type, and mcp.servers is the servers' object
	for name, raw := range doc.MCP {
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		if name != "servers" && m["type"] == nil {
			t.Errorf("mcp.%s has no type:\n%s", name, read(t, oc))
		}
	}
	for name, m := range servers {
		if m["type"] == nil {
			t.Errorf("mcp.servers.%s has no type:\n%s", name, read(t, oc))
		}
	}
}
