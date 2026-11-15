package library

// A command that an agent in WSL starts runs in its distro: `node
// /home/me/tools/x.js` is a Linux program there, which on Windows is
// another node or none at all (loong on Discord: a server his agents in
// WSL start fine was checked on Windows, where it "exited (1)"). So the
// check starts a command where the agents given it start it: on this
// machine for its own agents, and through wsl.exe (wslrun, as uni-switch runs
// a distro's Claude Code) in each distro whose agents have it.

import (
	"context"
	"os/exec"
	"slices"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"github.com/dieqiyun/uni-switch/backend/internal/wslrun"
)

// wslCommand starts a check's command in t's distro; a var for tests.
var wslCommand = func(ctx context.Context, t wslrun.Tool, args ...string) *exec.Cmd {
	return t.Command(ctx, args...)
}

// checkSides are where s's command is checked: "" for this machine, a
// distro's name for its agents there, this machine first. Everything is
// checked here for a server no agent has, with no WSL to start one in or
// Settings' Detect agents in WSL off, and for a Windows program (uni-switch's
// own image server, the one an agent in WSL is given, which runs on
// Windows whoever starts it). Nothing here asks WSL anything.
func checkSides(s *Server) []string {
	here := false
	var distros []string
	for _, id := range s.Agents {
		_, d, ok := strings.Cut(id, "@wsl:")
		if !ok || d == "" {
			here = true
		} else if !slices.Contains(distros, d) {
			distros = append(distros, d)
		}
	}
	if len(distros) == 0 || !wslrun.On || windowsPath.MatchString(s.Command) || settings.Shared().NoWSLAgents {
		return []string{""}
	}
	slices.Sort(distros)
	if here {
		return append([]string{""}, distros...)
	}
	return distros
}
