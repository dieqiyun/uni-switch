package library

import (
	"context"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// A server taken out of an agent's config keeps running in an agent that
// doesn't read its config again until it restarts: Codex, the Codex app
// and Claude Code. With every server turned off in the Library, one user's
// agents went on holding 17.75 GB of node processes for them, one set more
// for each Codex session (#3885). So the processes of a stdio server taken
// out are ended, once no agent's config names that command any more.

// ending ends the processes of the servers a change took out, before the
// change returns, as uni-switch library mcp exits after it; tests keep it from
// ending anything on the machine running them.
var ending = endDropped

func endDropped(dropped []*Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, s := range unused(dropped, Targets()) {
		n, err := proc.EndTrees(ctx, launchedBy(s))
		if err != nil {
			log.Printf("library: ending %s's processes: %v", s.Name, err)
		} else if n > 0 {
			log.Printf("library: ended %d process trees of %s, taken out of every agent", n, s.Name)
		}
	}
}

// unused are the dropped stdio servers no agent's config runs any more, by
// command and arguments: one the user has under another name, or another
// agent still has, keeps running.
func unused(dropped []*Server, all []*Target) []*Server {
	held := map[string]bool{}
	for _, t := range all {
		if t.MCP == nil {
			continue
		}
		have, _ := t.MCP.read()
		for _, s := range have {
			held[launchKey(s)] = true
		}
	}
	var out []*Server
	for _, s := range dropped {
		k := launchKey(s)
		if k != "" && !held[k] && !slices.ContainsFunc(out, func(o *Server) bool { return launchKey(o) == k }) {
			out = append(out, s)
		}
	}
	return out
}

// launchKey is a stdio server's command and arguments as launchedBy reads
// them; "" for a server with no command, or one too plain to tell its
// processes by (an interpreter with nothing after it).
func launchKey(s *Server) string {
	if s == nil || s.Command == "" || s.URL != "" {
		return ""
	}
	name := progName(s.Command)
	args := words(strings.Join(s.Args, " "))
	if len(args) == 0 && interpreters[name] {
		return ""
	}
	return name + "\x00" + strings.Join(args, "\x00")
}

var interpreters = map[string]bool{
	"node": true, "npx": true, "npm": true, "pnpm": true, "bunx": true, "bun": true, "deno": true,
	"uv": true, "uvx": true, "python": true, "python3": true, "py": true, "pipx": true,
	"docker": true, "podman": true, "sh": true, "bash": true, "zsh": true, "cmd": true,
	"powershell": true, "pwsh": true, "java": true, "go": true, "ruby": true,
}

// launchedBy picks a command line that runs s: its arguments last, with
// its command's program before them. It reads npx -y x run as
// `cmd /d /s /c "npx -y x"` and as `node …\npx-cli.js -y x` alike, the
// wrapper and the node it starts for Codex on Windows.
func launchedBy(s *Server) func(string) bool {
	key := launchKey(s)
	name := progName(s.Command)
	args := words(strings.Join(s.Args, " "))
	return func(line string) bool {
		if key == "" {
			return false
		}
		w := words(line)
		if len(w) <= len(args) || !slices.EqualFunc(w[len(w)-len(args):], args, strings.EqualFold) {
			return false
		}
		before := w[:len(w)-len(args)]
		if len(args) == 0 {
			return progName(before[len(before)-1]) == name
		}
		return slices.ContainsFunc(before, func(x string) bool { return progName(x) == name })
	}
}

// words is a command line split at spaces, its quotes dropped.
func words(line string) []string {
	return strings.Fields(strings.ReplaceAll(line, `"`, " "))
}

// progName is a program's name, lowercased, with no folder, extension or
// -cli: npx for C:\Program Files\nodejs\node_modules\npm\bin\npx-cli.js.
func progName(p string) string {
	p = strings.ToLower(p[strings.LastIndexAny(p, `/\`)+1:])
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1", ".js", ".cjs", ".mjs"} {
		p = strings.TrimSuffix(p, ext)
	}
	return strings.TrimSuffix(p, "-cli")
}
