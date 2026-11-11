package proc

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// #2685: an MCP server started by fnm's npx.cmd, by its full path, runs
// the package it fetched, which runs `node` by name. A terminal's fnm put
// node's folder on PATH; uni-switch's PATH, the registry's, didn't have it,
// and the server exited 1 with cmd's "'node' is not recognized as an
// internal or external command, operable program or batch file." After
// UserPath the child finds fnm's node, and FindTool finds a bare npx.
func TestUserPathFnmNode(t *testing.T) {
	home := t.TempDir()
	appdata := filepath.Join(home, "AppData", "Roaming")
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", appdata)
	t.Setenv("FNM_DIR", "")
	sys := filepath.Join(os.Getenv("SystemRoot"), "System32")
	t.Setenv("PATH", sys)
	root := filepath.Join(appdata, "fnm")
	inst := filepath.Join(root, "node-versions", "v24.19.0", "installation")
	if err := os.MkdirAll(inst, 0o755); err != nil {
		t.Fatal(err)
	}
	// npx's package bin, as npm's cmd shim runs it: node from PATH
	npx := filepath.Join(inst, "npx.cmd")
	for p, s := range map[string]string{npx: "@echo off\r\nnode %*\r\n", filepath.Join(inst, "node.cmd"): "@echo fnm node %*\r\n"} {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(root, "aliases", "default")
	if err := os.MkdirAll(filepath.Dir(alias), 0o755); err != nil {
		t.Fatal(err)
	}
	// a junction, as fnm makes it
	if out, err := Command("cmd", "/c", "mklink", "/J", alias, inst).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %v %s", err, out)
	}
	run := func() (string, error) {
		cmd := Command(npx, "-y", "chrome-devtools-mcp@latest")
		cmd.Dir = t.TempDir() // cmd looks in its folder before PATH
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	if out, err := run(); err == nil {
		t.Fatalf("before UserPath node was found already (%q): the test proves nothing", out)
	}

	UserPath()
	if dirs := filepath.SplitList(os.Getenv("PATH")); !slices.Contains(dirs, alias) {
		t.Fatalf("PATH after UserPath = %q, want fnm's default alias %q on it", dirs, alias)
	}
	if out, err := run(); err != nil || out != "fnm node -y chrome-devtools-mcp@latest" {
		t.Fatalf("npx.cmd by its full path: %v %q, want fnm's node run", err, out)
	}
	t.Setenv("PATH", sys)
	Forget()
	if got := FindTool("npx"); !strings.EqualFold(got, filepath.Join(alias, "npx.cmd")) {
		t.Fatalf("FindTool(npx) = %q, want the default alias's %q", got, filepath.Join(alias, "npx.cmd"))
	}
}
