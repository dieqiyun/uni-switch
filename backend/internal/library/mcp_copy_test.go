package library

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/mcpauth/mcpauthtest"
)

// #1478 (xiaozhu1337): a project uni-switch doesn't keep gets the library's
// servers by pasting them. What is copied for an agent is, byte for byte,
// the file uni-switch writes into a fresh project of its own for that agent,
// and a server the agent can't reach that way is said, not left in.
func TestConfigOfServersIsTheProjectFile(t *testing.T) {
	sandbox(t)
	ok(t)(SaveServer("", Server{Name: "fs", Transport: "stdio", Command: "npx", Args: []string{"-y", "fs"}, Env: map[string]string{"ROOT": "${HOME}/src"}}))
	ok(t)(SaveServer("", Server{Name: "docs", Transport: "http", URL: "https://docs.example/mcp", Headers: map[string]string{"Authorization": "Bearer ${DOCS_TOKEN}"}}))
	ok(t)(SaveServer("", Server{Name: "feed", Transport: "sse", URL: "https://feed.example/sse"}))
	proj := filepath.Join(t.TempDir(), "app")
	write(t, filepath.Join(proj, "README"), "app\n")
	ok(t)(AddProject(proj))
	names := []string{"docs", "feed", "fs"}
	for _, n := range names {
		// a server an agent can't take is said in the result's problems
		if _, err := ProjectServer(proj, n, projectAgents()); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range projectAgents() {
		c, err := ConfigOfServers(id, names)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if c.File != ProjectMCPFile(id) {
			t.Errorf("%s: file %q, want %q", id, c.File, ProjectMCPFile(id))
		}
		want := read(t, filepath.Join(proj, filepath.FromSlash(c.File)))
		// left out where a project of uni-switch's leaves it out: SSE for
		// Codex and Pi, a reference to a variable where it can't be one
		var skip []string
		f := &mcpFile{Format: projectMCPFiles[id].format}
		for _, n := range names {
			if f.supports(serverNamed(t, n)) != nil {
				skip = append(skip, n)
			}
		}
		if !slices.Equal(c.Skipped, skip) {
			t.Errorf("%s left out %v, want %v", id, c.Skipped, skip)
		}
		if id == "codex" && !slices.Contains(skip, "feed") {
			t.Errorf("codex took an SSE server")
		}
		if c.Text != want {
			t.Errorf("%s: copied\n%s\nbut a project of uni-switch's has in %s\n%s", id, c.Text, c.File, want)
		}
		if !slices.Contains(skip, "docs") && !strings.Contains(c.Text, "docs.example") {
			t.Errorf("%s: no docs server in\n%s", id, c.Text)
		}
	}
	// one uni-switch is signed in to is reached through the gateway, as a
	// project of uni-switch's is given it (#615)
	fk := mcpauthtest.New(t)
	if _, err := SaveServer("", Server{Name: "neon", Transport: "http", URL: fk.URL}); err != nil {
		t.Fatal(err)
	}
	fk.SignIn(t, "neon")
	if c, err := ConfigOfServers("claude", []string{"neon"}); err != nil || !strings.Contains(c.Text, "/mcp/neon") || strings.Contains(c.Text, fk.URL) {
		t.Errorf("a signed-in server: %+v %v", c, err)
	}
	if _, err := ConfigOfServers("claude", []string{"nope"}); err == nil {
		t.Error("a server the library hasn't was copied")
	}
	if _, err := ConfigOfServers("copilot", names); err == nil {
		t.Error("an agent with no project file was given one")
	}
	if c, err := ConfigOfServers("codex", []string{"feed"}); err != nil || c.Text != "" || !slices.Equal(c.Skipped, []string{"feed"}) {
		t.Errorf("only an SSE server, for Codex: %+v %v", c, err)
	}
}

func serverNamed(t *testing.T, name string) *Server {
	t.Helper()
	l, err := load()
	if err != nil {
		t.Fatal(err)
	}
	return l.server(name)
}

func projectAgents() []string {
	var ids []string
	for id := range projectMCPFiles {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
