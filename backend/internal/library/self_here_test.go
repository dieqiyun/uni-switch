package library

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

// thisuni-switch makes exe the uni-switch this computer runs.
func thisUniSwitch(t *testing.T, exe string) {
	t.Helper()
	old := selfExe
	selfExe = func() (string, error) { return exe, nil }
	t.Cleanup(func() { selfExe = old })
}

// seedLibrary writes library.json as a uni-switch before #1439 left it, or
// as a sync from another computer brought it: the servers as they are.
func seedLibrary(t *testing.T, servers ...Server) {
	t.Helper()
	l := Library{MCP: []*Server{}, Applied: map[string]*Applied{}}
	for i := range servers {
		l.MCP = append(l.MCP, &servers[i])
	}
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	write(t, path(), string(b))
}

// codexCommand is the command Codex's config.toml starts server name with.
func codexCommand(t *testing.T, h, name string) string {
	t.Helper()
	var c struct {
		Servers map[string]struct {
			Command string `toml:"command"`
		} `toml:"mcp_servers"`
	}
	if err := toml.Unmarshal([]byte(read(t, filepath.Join(h, ".codex", "config.toml"))), &c); err != nil {
		t.Fatal(err)
	}
	return c.Servers[name].Command
}

// storedCommand is the command library.json keeps for server name.
func storedCommand(t *testing.T, name string) string {
	t.Helper()
	var l Library
	if err := json.Unmarshal([]byte(read(t, path())), &l); err != nil {
		t.Fatal(err)
	}
	for _, s := range l.MCP {
		if s.Name == name {
			return s.Command
		}
	}
	t.Fatalf("library.json has no %s", name)
	return ""
}

// #1439: uni-switch-image was kept with one computer's uni-switch by full path. A
// sync carried C:\Users\…\Downloads\uni-switch-windows-amd64.exe to a Mac, a
// Mac's app to Windows, and a uni-switch moved out of Downloads left its old
// path behind: the page said it couldn't start, and the agents were given
// a program that isn't there. The library now gives this computer's uni-switch,
// to the agents and to the check, and keeps it.
func TestOwnServerFromElsewhereRunsThisUniSwitch(t *testing.T) {
	for _, c := range []struct{ name, stored string }{
		{"Windows' uni-switch", `C:\Users\xiaoqian\Downloads\uni-switch-windows-amd64.exe`},
		{"a Mac's uni-switch", "/Volumes/gone-1439/uni-switch.app/Contents/MacOS/uni-switch"},
		{"moved out of Downloads", ""},
		{"a sync from a uni-switch that names it", "uni-switch"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := sandbox(t)
			here := filepath.Join(h, "Programs", "uni-switch", "uni-switch-windows-amd64.exe")
			write(t, here, "")
			thisUniSwitch(t, here)
			stored := c.stored
			if stored == "" { // it was here, in Downloads, and has gone
				stored = filepath.Join(h, "Downloads", "uni-switch-windows-amd64.exe")
				write(t, stored, "")
				if err := os.Remove(stored); err != nil {
					t.Fatal(err)
				}
			}
			seedLibrary(t, Server{Name: selfServerName, Transport: "stdio", Command: stored, Args: []string{"mcp", "image"}, Agents: []string{"codex"}})

			// the page and the check read it as this computer's
			v, err := load()
			if err != nil {
				t.Fatal(err)
			}
			if got := v.server(selfServerName).Command; got != here {
				t.Fatalf("read as %q, want this uni-switch %q", got, here)
			}
			// the agents are given it, and library.json keeps it
			ok(t)(Sync())
			if got := codexCommand(t, h, selfServerName); got != here {
				t.Errorf("Codex starts %q, want %q", got, here)
			}
			if got := storedCommand(t, selfServerName); got != here {
				t.Errorf("library.json keeps %q, want %q", got, here)
			}
			// and once it is, a sync has nothing to write
			if res := ok(t)(Sync()); len(res.Changed) != 0 {
				t.Errorf("a second sync changed %v", res.Changed)
			}
		})
	}
}

// Only uni-switch's own is pointed at this uni-switch: a server of the user's that
// runs another program, one by a name like uni-switch's that isn't uni-switch's
// MCP server, or a uni-switch build that is on this computer stays as it is,
// in the library and in the agents, gone or not.
func TestUsersOwnServerStaysAsItIs(t *testing.T) {
	h := sandbox(t)
	here := filepath.Join(h, "Programs", "uni-switch")
	write(t, here, "")
	thisUniSwitch(t, here)
	otherBuild := filepath.Join(h, "dev", "uni-switch-dev")
	write(t, otherBuild, "")
	mine := map[string]string{
		"gone-tool":      `C:\Users\me\tools\imagetool.exe`, // mcp image, but not uni-switch
		"uni-switch-cli": `C:\Users\me\tools\uni-switch-helper.exe`,
		"pinned-build":   otherBuild,
		"npx-server":     "npx",
	}
	seedLibrary(t,
		Server{Name: "gone-tool", Transport: "stdio", Command: mine["gone-tool"], Args: []string{"mcp", "image"}, Agents: []string{"codex"}},
		Server{Name: "uni-switch-cli", Transport: "stdio", Command: mine["uni-switch-cli"], Args: []string{"serve"}, Agents: []string{"codex"}},
		Server{Name: "pinned-build", Transport: "stdio", Command: mine["pinned-build"], Args: []string{"mcp", "image"}, Agents: []string{"codex"}},
		Server{Name: "npx-server", Transport: "stdio", Command: "npx", Args: []string{"-y", "@mcp/fs"}, Agents: []string{"codex"}},
	)
	ok(t)(Sync())
	for name, want := range mine {
		if got := storedCommand(t, name); got != want {
			t.Errorf("%s: library.json has %q, want the user's %q", name, got, want)
		}
		if got := codexCommand(t, h, name); got != want {
			t.Errorf("%s: Codex starts %q, want the user's %q", name, got, want)
		}
	}
}
