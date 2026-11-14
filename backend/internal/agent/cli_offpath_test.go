package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// 悠悠哥 on Discord (macOS): the Agents page listed "Codex 0.162.1" under
// Not set up while `which -a codex` found nothing and ~/.codex wasn't
// there. uni-switch adds the folders users' tools go in to its own PATH
// (proc.UserPath: nvm's newest Node among them), so a codex installed with
// npm under a Node the terminal no longer uses is found, and runs, though
// no terminal finds it. It is still listed, but its CLI now says where it
// is and that a terminal's PATH doesn't have that folder.
func TestCLIFoundOffTerminalPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", "")
	was := appFolders
	appFolders = func() []string { return nil }
	t.Cleanup(func() { appFolders = was })

	// npm i -g @openai/codex under nvm's v22.12.0: its bin links into the
	// package, whose package.json has the version
	node := filepath.Join(home, ".nvm", "versions", "node", "v22.12.0")
	pkg := filepath.Join(node, "lib", "node_modules", "@openai", "codex")
	if err := os.MkdirAll(filepath.Join(pkg, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"name":"@openai/codex","version":"0.162.1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "bin", "codex.js"), []byte("#!/bin/sh\necho codex-cli 0.162.1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	bindir := filepath.Join(node, "bin")
	if err := os.MkdirAll(bindir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(bindir, exe("codex"))
	if err := os.Symlink(filepath.Join("..", "lib", "node_modules", "@openai", "codex", "bin", "codex.js"), bin); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	// uni-switch's PATH after UserPath has nvm's folder; the terminal's hasn't
	t.Setenv("PATH", bindir)
	terminal := []string{filepath.Join(home, "elsewhere"), "/usr/bin", "/bin"}
	wasLogin := loginPath
	t.Cleanup(func() { loginPath = wasLogin })
	loginPath = func() []string { return terminal }

	dir := filepath.Join(home, ".codex")
	codex := &Agent{ID: "codex", Name: "Codex", Bin: "codex", Dir: dir, Path: filepath.Join(dir, "config.toml")}
	if !codex.Detected() {
		t.Fatal("codex on uni-switch's PATH not detected")
	}
	c, ok := codex.CLI()
	if !ok || c.Version != "0.162.1" {
		t.Fatalf("CLI %+v, %v; want version 0.162.1", c, ok)
	}
	if c.Path != bin || !c.OffPath {
		t.Fatalf("CLI found at %q, off the terminal's PATH %v; want %q, true", c.Path, c.OffPath, bin)
	}

	// the terminal has the folder: nothing more to say
	terminal = append(terminal, bindir)
	if c, _ := codex.CLI(); c.Path != bin || c.OffPath {
		t.Fatalf("on the terminal's PATH: %+v", c)
	}
	// a terminal whose PATH can't be told says nothing either
	terminal = nil
	if c, _ := codex.CLI(); c.OffPath {
		t.Fatalf("terminal's PATH unknown: %+v", c)
	}
}
