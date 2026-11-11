package library

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/tidwall/jsonc"
)

// lc on Discord: Zed wasn't among the agents an MCP server could be given
// to. Zed reads them from context_servers in its settings.json (JSONC),
// the file uni-switch sets its model in: a command as command/args/env, a url
// as url/headers, which it speaks streamable HTTP to (zed-industries/zed
// 2c99f547, crates/settings_content/src/project.rs
// ContextServerSettingsContent). The user's comments, other settings, own
// servers and an extension's server are kept; unchecking Zed takes out
// only uni-switch's entries.
func TestZedMCP(t *testing.T) {
	user := `// Zed settings
//
// For information on how to configure Zed, see the Zed
// documentation: https://zed.dev/docs/configuring-zed
{
  "theme": "One Dark", // my theme
  /* the servers I added in Zed */
  "context_servers": {
    "mine": {
      "enabled": false,
      "command": "uvx",
      "args": ["mine"],
      "env": {},
      "timeout": 120,
    },
    "remote-mine": {
      "url": "https://mine.example.com/mcp",
      "headers": { "Authorization": "Bearer <token>" }
    },
    // installed from Zed's extensions
    "mcp-server-github": {
      "settings": { "github_personal_access_token": "ghp_x" }
    },
  },
  "buffer_font_size": 15,
}
`
	for _, c := range []struct {
		name string
		dir  func(h string) string // Zed's settings folder; nil: the default
	}{
		{name: "default"},
		{name: "UNI_SWITCH_ZED_CONFIG_DIR", dir: func(h string) string { return filepath.Join(h, "zedg") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := sandbox(t)
			dir := filepath.Join(h, ".config", "zed")
			if runtime.GOOS == "windows" {
				dir = filepath.Join(h, "AppData", "Roaming", "Zed")
			}
			if c.dir != nil {
				dir = c.dir(h)
				t.Setenv("UNI_SWITCH_ZED_CONFIG_DIR", dir)
			}
			p := filepath.Join(dir, "settings.json")
			write(t, p, user)

			tg := targetByID("zed")
			if tg == nil || tg.MCP == nil || tg.MCP.Path != p {
				t.Fatalf("Zed has no MCP target at %s: %+v", p, tg)
			}
			if id, err := Takes("zed", "mcp"); id != "zed" || err != nil {
				t.Errorf("Takes: %q %v", id, err)
			}
			parse := func() map[string]map[string]any {
				t.Helper()
				var doc map[string]any
				if err := json.Unmarshal(jsonc.ToJSON([]byte(read(t, p))), &doc); err != nil {
					t.Fatalf("%v:\n%s", err, read(t, p))
				}
				out := map[string]map[string]any{}
				all, _ := doc["context_servers"].(map[string]any)
				for k, v := range all {
					out[k], _ = v.(map[string]any)
				}
				return out
			}

			// the user's own are there to bring in; the extension's isn't one
			l, err := load()
			if err != nil {
				t.Fatal(err)
			}
			found := map[string]*Server{}
			for _, f := range foundServers(l) {
				if slices.Contains(f.Server.Agents, "zed") {
					found[f.Server.Name] = f.Server
				}
			}
			if s := found["mine"]; s == nil || s.Transport != "stdio" || s.Command != "uvx" {
				t.Errorf("mine: %+v", s)
			}
			if s := found["remote-mine"]; s == nil || s.Transport != "http" || s.URL != "https://mine.example.com/mcp" {
				t.Errorf("remote-mine: %+v", s)
			}
			if found["mcp-server-github"] != nil {
				t.Errorf("an extension's server was found as one to bring in")
			}

			// the page: Zed's chip, greyed for SSE and for ${NAME}
			v, err := Read(nil)
			if err != nil {
				t.Fatal(err)
			}
			i := slices.IndexFunc(v.Agents, func(a AgentView) bool { return a.ID == "zed" })
			if i < 0 || v.Agents[i].MCP != p || !v.Agents[i].NoSSE || !v.Agents[i].NoEnvRefs || v.Agents[i].NoRemote {
				t.Errorf("Zed on the page: %+v", v.Agents)
			}

			agents := []string{"zed"}
			fs := Server{Name: "fs", Transport: "stdio", Command: "npx", Args: []string{"-y", "@mcp/fs"}, Env: map[string]string{"K": "V"}, Agents: agents}
			web := Server{Name: "web", Transport: "http", URL: "https://example.com/mcp", Headers: map[string]string{"Authorization": "Bearer x"}, Agents: agents}
			ok(t)(SaveServer("", fs))
			ok(t)(SaveServer("", web))
			for _, refused := range []struct {
				s   Server
				why string
			}{
				{Server{Name: "old", Transport: "sse", URL: "https://example.com/sse", Agents: agents}, errNoSSE.Error()},
				{Server{Name: "gh", Transport: "http", URL: "https://example.com/gh", Headers: map[string]string{"Authorization": "Bearer ${GH_TOKEN}"}, Agents: agents}, errNoEnvRef.Error()},
				{Server{Name: "mcp-server-github", Transport: "stdio", Command: "github-mcp", Agents: agents}, errZedExtension.Error()},
			} {
				res, err := SaveServer("", refused.s)
				if err != nil {
					t.Fatal(err)
				}
				if !slices.ContainsFunc(res.Problems, func(p Problem) bool { return p.What == "mcp:"+refused.s.Name && p.Error == refused.why }) {
					t.Errorf("%s: %+v", refused.s.Name, res.Problems)
				}
			}

			got := parse()
			for name, want := range map[string]map[string]any{
				"fs":  {"command": "npx", "args": []any{"-y", "@mcp/fs"}, "env": map[string]any{"K": "V"}},
				"web": {"url": "https://example.com/mcp", "headers": map[string]any{"Authorization": "Bearer x"}},
				// the user's, as they were
				"mine":              {"enabled": false, "command": "uvx", "args": []any{"mine"}, "env": map[string]any{}, "timeout": float64(120)},
				"remote-mine":       {"url": "https://mine.example.com/mcp", "headers": map[string]any{"Authorization": "Bearer <token>"}},
				"mcp-server-github": {"settings": map[string]any{"github_personal_access_token": "ghp_x"}},
			} {
				g, _ := json.Marshal(got[name])
				w, _ := json.Marshal(want)
				if string(g) != string(w) {
					t.Errorf("%s:\n got %s\nwant %s", name, g, w)
				}
			}
			for _, name := range []string{"old", "gh"} {
				if got[name] != nil {
					t.Errorf("%s was written: %v", name, got[name])
				}
			}
			back, err := tg.MCP.read()
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []Server{fs, web} {
				if s := back[want.Name]; s == nil || !s.same(&want) {
					t.Errorf("%s read back as %+v", want.Name, s)
				}
			}
			// what Zed can't take is no problem once it isn't given to Zed
			ok(t)(RemoveServers([]string{"old", "gh", "mcp-server-github"}))
			if parse()["mcp-server-github"]["settings"] == nil {
				t.Errorf("the extension's server: %v", parse()["mcp-server-github"])
			}
			before := read(t, p)
			ok(t)(Sync())
			if read(t, p) != before {
				t.Errorf("a sync rewrote servers that are as the library has them:\n%s", read(t, p))
			}

			// a key the user gives uni-switch's entry in Zed stays
			text := strings.Replace(read(t, p), `"command": "npx"`, `"enabled": false, "command": "npx"`, 1)
			write(t, p, text)
			fs.Args = []string{"-y", "@mcp/fs", "/tmp"}
			ok(t)(SaveServer("fs", fs))
			if g := parse()["fs"]; g["enabled"] != false || len(g["args"].([]any)) != 3 {
				t.Errorf("fs after an edit: %v", g)
			}

			// unchecking Zed takes out only uni-switch's
			ok(t)(ServerAgents("fs", nil))
			ok(t)(RemoveServer("web"))
			got = parse()
			if len(got) != 3 || got["mine"] == nil || got["remote-mine"] == nil || got["mcp-server-github"]["settings"] == nil {
				t.Errorf("after taking uni-switch's out:\n%s", read(t, p))
			}
			for _, keep := range []string{"// Zed settings", "// my theme", "/* the servers I added in Zed */", "// installed from Zed's extensions", `"theme": "One Dark"`, `"buffer_font_size": 15`, `"timeout": 120`} {
				if !strings.Contains(read(t, p), keep) {
					t.Errorf("lost %s:\n%s", keep, read(t, p))
				}
			}
		})
	}
}
