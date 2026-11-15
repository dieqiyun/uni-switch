package library

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// #1618 (sxwedo): a server an agent has of its own, listed under "In your
// agents", could only be brought into the library. Removed where it is, it
// leaves every agent that has it as it is: Claude Code's ~/.claude.json
// keeps the user's other keys and server, Codex's config.toml its comment,
// its settings and the user's other table, OpenCode's JSONC its comments.
// Each file is in the backups as it was. Gemini CLI's other server by
// that name stays, and so does a file that doesn't parse.
func TestRemoveFoundServer(t *testing.T) {
	h := sandbox(t)
	cj := filepath.Join(h, ".claude.json")
	claudeWas := `{
  "numStartups": 12,
  "projects": {"/src/app": {"allowedTools": ["Bash"]}},
  "mcpServers": {
    "gone": {"command": "gone-mcp", "args": ["--x"], "env": {"K": "V"}},
    "mine": {"command": "my-mcp", "timeout": 30}
  },
  "userID": "abc"
}
`
	write(t, cj, claudeWas)
	codex := filepath.Join(h, ".codex/config.toml")
	write(t, codex, `# my settings
model = "gpt-6"

[mcp_servers.gone]
command = "gone-mcp"
args = ["--x"]

[mcp_servers.gone.env]
K = "V"

[mcp_servers.mine]
command = "my-mcp"
startup_timeout_sec = 20

[profiles.fast]
model = "gpt-6-mini"
`)
	oc := filepath.Join(h, ".config/opencode/opencode.json")
	write(t, oc, `{
  // the theme I like
  "theme": "tokyonight",
  "mcp": {
    "gone": {"type": "local", "command": ["gone-mcp", "--x"], "environment": {"K": "V"}, "enabled": true},
    // mine
    "mine": {"type": "local", "command": ["my-mcp"], "enabled": true}
  }
}
`)
	gem := filepath.Join(h, ".gemini/settings.json")
	gemWas := `{"theme": "Dark", "mcpServers": {"gone": {"command": "another-gone"}}}` + "\n"
	write(t, gem, gemWas)
	// Cursor's doesn't parse: what it holds is unknown, so it isn't written
	cursor := filepath.Join(h, ".cursor/mcp.json")
	cursorWas := `{"mcpServers": {"gone": {"command": "gone-mcp", "args": ["--x"]` + "\n"
	write(t, cursor, cursorWas)

	f := foundOf(t, "gone")
	if !slices.Equal(f.Server.Agents, []string{"claude", "codex", "opencode"}) || !slices.Equal(f.Others, []string{"gemini"}) {
		t.Fatalf("found before: agents %v others %v", f.Server.Agents, f.Others)
	}
	if _, err := RemoveFoundServer("nope"); err == nil {
		t.Error("a server no agent has was taken")
	}
	res := ok(t)(RemoveFoundServer("gone"))

	if s := servers(t, cj, "mcpServers"); len(s) != 1 || s["mine"] == nil {
		t.Errorf("claude's servers after: %v", s)
	}
	c := jsonOf(t, cj)
	if c["numStartups"] != float64(12) || c["userID"] != "abc" || c["projects"] == nil {
		t.Errorf("claude's own keys after: %v", c)
	}
	if mine, _ := servers(t, cj, "mcpServers")["mine"].(map[string]any); mine["timeout"] != float64(30) {
		t.Errorf("the user's key in their own server: %v", mine)
	}
	got := read(t, codex)
	for _, keep := range []string{"# my settings", `model = "gpt-6"`, "[mcp_servers.mine]", "startup_timeout_sec = 20", "[profiles.fast]"} {
		if !strings.Contains(got, keep) {
			t.Errorf("codex lost %q:\n%s", keep, got)
		}
	}
	if strings.Contains(got, "gone") {
		t.Errorf("codex still has it:\n%s", got)
	}
	got = read(t, oc)
	for _, keep := range []string{"// the theme I like", `"theme": "tokyonight"`, "// mine", `"mine"`} {
		if !strings.Contains(got, keep) {
			t.Errorf("opencode lost %q:\n%s", keep, got)
		}
	}
	if strings.Contains(got, "gone") {
		t.Errorf("opencode still has it:\n%s", got)
	}
	if read(t, gem) != gemWas {
		t.Errorf("gemini's other gone was touched:\n%s", read(t, gem))
	}
	if read(t, cursor) != cursorWas {
		t.Errorf("cursor's file that doesn't parse was written:\n%s", read(t, cursor))
	}

	// each as it was, in this change's backups
	if res.Backup == "" {
		t.Fatal("no backup said")
	}
	for _, b := range []struct{ agent, file, was string }{{"claude", ".claude.json", claudeWas}} {
		if got := read(t, filepath.Join(res.Backup, b.agent, b.file)); got != b.was {
			t.Errorf("%s's backup: %q", b.agent, got)
		}
	}
	for _, p := range []string{"codex/config.toml", "opencode/opencode.json"} {
		if read(t, filepath.Join(res.Backup, p)) == "" {
			t.Errorf("%s isn't in the backups", p)
		}
	}
	if m, _ := filepath.Glob(filepath.Join(res.Backup, "*", "settings.json")); len(m) != 0 {
		t.Errorf("gemini's file, not written, was backed up: %v", m)
	}

	// what's found now is Gemini's
	f = foundOf(t, "gone")
	if !slices.Equal(f.Server.Agents, []string{"gemini"}) || f.Server.Command != "another-gone" {
		t.Errorf("found after: %+v", f.Server)
	}
}

// A file that stops parsing after the server was found stops the removal
// before anything is written: two agents have it, and neither file is
// changed when one can't be read.
func TestRemoveFoundServerUnreadable(t *testing.T) {
	h := sandbox(t)
	cj := filepath.Join(h, ".claude.json")
	write(t, cj, `{"mcpServers": {"gone": {"command": "gone-mcp"}}}`+"\n")
	gem := filepath.Join(h, ".gemini/settings.json")
	write(t, gem, `{"mcpServers": {"gone": {"command": "gone-mcp"}}}`+"\n")
	mf := targetByID("gemini").MCP
	l, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if fs := foundServers(l); len(fs) != 1 || len(fs[0].Server.Agents) != 2 {
		t.Fatalf("found: %+v", fs)
	}
	write(t, gem, `{"mcpServers": {"gone": `)
	if err := removeFoundEntry(mf, "gemini", "gone", newBackups()); err == nil {
		t.Error("a file that doesn't parse was taken as having nothing")
	}
	if got := read(t, gem); got != `{"mcpServers": {"gone": ` {
		t.Errorf("written: %q", got)
	}
}

// A server the agent's app writes into its config each time it starts
// (Codex's node_repl) isn't removed: it would be back at the next start.
func TestRemoveFoundServerOwn(t *testing.T) {
	h := sandbox(t)
	codex := filepath.Join(h, ".codex/config.toml")
	was := "[mcp_servers.node_repl]\ncommand = \"/Applications/Codex.app/Contents/Resources/node_repl\"\n"
	write(t, codex, was)
	if !foundOf(t, "node_repl").Own {
		t.Fatal("not Codex's own")
	}
	if _, err := RemoveFoundServer("node_repl"); err == nil {
		t.Error("the app's own server was taken out")
	}
	if read(t, codex) != was {
		t.Errorf("written:\n%s", read(t, codex))
	}
}

// Every agent whose MCP file uni-switch reads: a server of the user's there is
// found, and removed from each file it is in, in that agent's own format;
// the user's other server there, and a key of theirs in it, stay.
func TestRemoveFoundServerEveryAgent(t *testing.T) {
	sandbox(t)
	gone := &Server{Name: "gone", Transport: "stdio", Command: "gone-mcp", Args: []string{"--x"}}
	mine := &Server{Name: "mine", Transport: "stdio", Command: "my-mcp"}
	var with []string
	for _, tg := range Targets() {
		if tg.MCP == nil {
			continue
		}
		if err := tg.MCP.put(mine, map[string]any{"note": "the user's"}); err != nil {
			t.Fatalf("%s: %v", tg.Agent.ID, err)
		}
		if err := tg.MCP.put(gone, nil); err != nil {
			t.Fatalf("%s: %v", tg.Agent.ID, err)
		}
		with = append(with, tg.Agent.ID)
	}
	if len(with) < 10 {
		t.Fatalf("only %v have an MCP file here", with)
	}
	f := foundOf(t, "gone")
	all := slices.Sorted(slices.Values(slices.Concat(f.Server.Agents, f.Others)))
	if !slices.Equal(all, slices.Sorted(slices.Values(with))) {
		t.Fatalf("found in %v, others %v; written to %v", f.Server.Agents, f.Others, with)
	}
	t.Logf("found in %v, others %v", f.Server.Agents, f.Others)
	ok(t)(RemoveFoundServer("gone"))
	for _, tg := range Targets() {
		if tg.MCP == nil {
			continue
		}
		have, err := tg.MCP.read()
		if err != nil {
			t.Fatalf("%s: %v", tg.Agent.ID, err)
		}
		if slices.Contains(f.Server.Agents, tg.Agent.ID) && have["gone"] != nil {
			t.Errorf("%s still has it in %s", tg.Agent.ID, tg.MCP.Path)
		}
		if have["mine"] == nil {
			t.Errorf("%s lost the user's other server", tg.Agent.ID)
		}
		es, _ := tg.MCP.entries()
		if e := es["mine"]; tg.MCP.Format != fmtDsh && (e == nil || e["note"] != "the user's") {
			t.Errorf("%s: the user's key in their server: %v", tg.Agent.ID, e)
		}
	}
}

// Every format uni-switch reads an agent's servers in, the agents a sandbox
// doesn't find included (Hermes, Zed, Kimi, Devin, Grok, Cline, …): the
// entry goes, the user's other server and their key in it stay, and the
// file is backed up first.
func TestRemoveFoundEntryEveryFormat(t *testing.T) {
	sandbox(t)
	gone := &Server{Name: "gone", Transport: "stdio", Command: "gone-mcp", Args: []string{"--x"}}
	mine := &Server{Name: "mine", Transport: "stdio", Command: "my-mcp"}
	for fm := fmtClaude; fm <= fmtZed; fm++ {
		if fm == fmtDsh {
			continue // its profiles' files: TestRemoveFoundServerEveryAgent
		}
		ext := ".json"
		switch fm {
		case fmtCodex, fmtGrok:
			ext = ".toml"
		case fmtGoose, fmtHermes:
			ext = ".yaml"
		}
		dir := t.TempDir()
		mf := &mcpFile{Path: filepath.Join(dir, "config"+ext), Format: fm}
		if err := mf.put(mine, map[string]any{"note": "the user's"}); err != nil {
			t.Fatalf("format %d: %v", fm, err)
		}
		if err := mf.put(gone, nil); err != nil {
			t.Fatalf("format %d: %v", fm, err)
		}
		if have, err := mf.read(); err != nil || have["gone"] == nil {
			t.Fatalf("format %d: not written: %v %v", fm, have, err)
		}
		was := read(t, mf.Path)
		b := newBackups()
		if err := removeFoundEntry(mf, "x", "gone", b); err != nil {
			t.Fatalf("format %d: %v", fm, err)
		}
		have, err := mf.read()
		if err != nil || have["gone"] != nil || have["mine"] == nil {
			t.Errorf("format %d after: %v %v\n%s", fm, have, err, read(t, mf.Path))
		}
		if es, _ := mf.entries(); es["mine"]["note"] != "the user's" {
			t.Errorf("format %d: the user's key: %v", fm, es["mine"])
		}
		if got := read(t, filepath.Join(b.dir, "x", filepath.Base(mf.Path))); got != was {
			t.Errorf("format %d: backup %q", fm, got)
		}
	}
}

// Pi finds servers in a file it is moving away from too (Extra), which
// pi-mcp-adapter 5 still reads: the server goes from there as well, or it
// would go on being found, and run.
func TestRemoveFoundEntryPiExtra(t *testing.T) {
	sandbox(t)
	dir := t.TempDir()
	mf := &mcpFile{Path: filepath.Join(dir, "mcp.json"), Format: fmtPiNative, Extra: []string{filepath.Join(dir, "mcp-adapter.json")}}
	old := &mcpFile{Path: mf.Extra[0], Format: fmtPiNative}
	gone := &Server{Name: "gone", Transport: "stdio", Command: "gone-mcp"}
	for _, f := range []*mcpFile{mf, old} {
		if err := f.put(&Server{Name: "mine", Transport: "stdio", Command: "my-mcp"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := old.put(gone, nil); err != nil {
		t.Fatal(err)
	}
	if have, _ := mf.read(); have["gone"] == nil {
		t.Fatal("not found in the other file")
	}
	b := newBackups()
	if err := removeFoundEntry(mf, "pi", "gone", b); err != nil {
		t.Fatal(err)
	}
	if have, _ := mf.read(); have["gone"] != nil || have["mine"] == nil {
		t.Errorf("after: %v", have)
	}
	if have, _ := old.read(); have["gone"] != nil || have["mine"] == nil {
		t.Errorf("the other file after: %v", have)
	}
	if read(t, filepath.Join(b.dir, "pi", "mcp-adapter.json")) == "" {
		t.Error("the other file isn't in the backups")
	}
	if read(t, filepath.Join(b.dir, "pi", "mcp.json")) != "" {
		t.Error("mcp.json, without the server, was backed up as if written")
	}

	// every file is read before any is written: the other one not parsing
	// leaves mcp.json as it was, not half done
	if err := mf.put(gone, nil); err != nil {
		t.Fatal(err)
	}
	before := read(t, mf.Path)
	write(t, old.Path, `{"mcpServers": {"gone": `)
	if err := removeFoundEntry(mf, "pi", "gone", newBackups()); err == nil {
		t.Error("the other file doesn't parse, and that wasn't said")
	}
	if got := read(t, mf.Path); got != before {
		t.Errorf("mcp.json written while the other file doesn't parse:\n%s", got)
	}
}

// foundOf is the server by that name in the agents, not in the library.
func foundOf(t *testing.T, name string) Found {
	t.Helper()
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range v.FoundServers {
		if f.Server.Name == name {
			return f
		}
	}
	t.Fatalf("%s isn't found: %+v", name, v.FoundServers)
	return Found{}
}
