// Package agentplug retains legacy descriptor shapes only. External agent modules are removed.
package agentplug

import (
	"github.com/dieqiyun/uni-switch/backend/internal/plugin"
	"path/filepath"
	"strings"
)

type Desc struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Config string   `json:"config"`
	Format string   `json:"format"`
	Model  string   `json:"model"`
	Prefix string   `json:"prefix"`
	Bin    string   `json:"bin"`
	Dir    string   `json:"dir"`
	UA     []string `json:"ua"`
	Icon   string   `json:"icon"`
	// Notice is advice shown after a change, as a built-in agent's is
	// (an agent that reads its config at start-up needs a restart)
	Notice string `json:"notice"`
	// Library is where the agent reads the Library's instructions, MCP
	// servers and skills
	Library Places `json:"library"`
	// Startup is the file and key of the model a new session starts on,
	// for an agent that keeps it apart from config; nil for one that
	// keeps it in config (Model) or picks it itself
	Startup *Startup `json:"startup"`
}

// Startup is where an agent keeps the model it starts on, apart from the
// file connect writes: Config and Format as Desc's are, Model the key
// path of the model in it, and Keys what is written beside it, as it is
// (PiG's defaultProvider, the provider id its connect writes).
type Startup struct {
	Config string         `json:"config"`
	Format string         `json:"format"`
	Model  string         `json:"model"`
	Keys   map[string]any `json:"keys"`
}

// Places are where an agent reads what the Library gives it, each a path
// as Config is (~ and $VAR expanded); an empty one is a thing it isn't
// given.
type Places struct {
	Instructions string `json:"instructions"`
	MCP          string `json:"mcp"`
	// MCPFormat is how its MCP file lists servers: one of MCPFormats
	MCPFormat string `json:"mcpFormat"`
	Skills    string `json:"skills"`
}

type Agent struct {
	Spec string
	File string
	Desc Desc
}

func List() ([]*Agent, map[string]string)                          { return nil, nil }
func Reload()                                                      {}
func Compile(spec, file string) (*Agent, error)                    { return nil, plugin.ErrUnavailable }
func (a *Agent) Connect(in map[string]any) (map[string]any, error) { return nil, plugin.ErrUnavailable }
func FormatOf(config string) string {
	switch strings.ToLower(filepath.Ext(config)) {
	case ".json":
		return "json"
	case ".jsonc":
		return "jsonc"
	case ".yml", ".yaml":
		return "yaml"
	case ".env":
		return "env"
	}
	return ""
}
