package library

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// ServersConfig is what an agent's project file would hold with the
// library's servers called names in it, as uni-switch writes them into a
// project it has (syncProjectMCP): for the user to paste into a project
// uni-switch doesn't keep (#1478, xiaozhu1337: 不建项目、一次性添加项目级
// MCP). File is the file in the project, Text the file whole, and Skipped
// the servers the agent can't reach that way (SSE for Codex or Pi).
type ServersConfig struct {
	File    string   `json:"file"`
	Text    string   `json:"text"`
	Skipped []string `json:"skipped,omitempty"`
}

// ConfigOfServers writes the servers into a fresh file in agent's project
// format, in a folder of its own, and reads it back: the same encoder, the
// same references to variables, a server uni-switch is signed in to through
// the gateway as a project of uni-switch's is given it.
func ConfigOfServers(agent string, names []string) (*ServersConfig, error) {
	pf, ok := projectMCPFiles[agent]
	if !ok {
		return nil, fmt.Errorf("uni-switch knows of no file %s reads a project's MCP servers from", agent)
	}
	if len(names) == 0 {
		return nil, errors.New("no servers named")
	}
	mu.Lock()
	l, err := load()
	mu.Unlock()
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "uni-switch-mcp-config-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	f := &mcpFile{Path: filepath.Join(tmp, filepath.FromSlash(pf.rel)), Format: pf.format}
	out := &ServersConfig{File: pf.rel}
	put := 0
	// in the order a project's file has them
	for _, name := range slices.Compact(slices.Sorted(slices.Values(names))) {
		s := l.server(name)
		if s == nil {
			return nil, fmt.Errorf("no server called %s", name)
		}
		if f.supports(s) != nil {
			out.Skipped = append(out.Skipped, name)
			continue
		}
		if err := f.put(through(s, gateway.URL()), nil); err != nil {
			return nil, err
		}
		put++
	}
	if put == 0 {
		return out, nil
	}
	b, err := os.ReadFile(f.Path)
	if err != nil {
		return nil, err
	}
	out.Text = string(b)
	return out, nil
}
