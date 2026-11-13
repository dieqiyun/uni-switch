// Package library is the Library page: instructions, MCP servers, skills
// and hooks, kept once and given to every agent. While it gives agents
// anything, it stays on.
package library

import (
	lib "github.com/dieqiyun/uni-switch/backend/internal/library"
	"github.com/dieqiyun/uni-switch/backend/internal/pluginrt"
)

func init() {
	pluginrt.Register(pluginrt.Manifest{
		ID: "library", API: "^1", Requires: []string{"window"},
		Name: "Library", Sub: "Instructions, MCP servers, skills and hooks, kept once and given to every agent",
		One: "1 item given to agents", Many: "{n} items given to agents",
		InUse:  lib.InUse,
		Parts:  []pluginrt.Part{{Slot: "nav.page", Key: "library"}},
		Client: []string{"i18n.js", "library.css", "client.js"},
	})
}
