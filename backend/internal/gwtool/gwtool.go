// Package gwtool is the gateway's tool point (P3 of
// docs/subsystems/plugin-runtime.md): the tools uni-switch answers itself for
// an agent, in place of the vendor. A plugin serves a tool by its name
// while it is on (Serve). In the process serving the gateway (Gate), a
// tool no plugin serves is neither offered nor answered; a process not
// serving the gateway (the CLI, the TUI) has them all, as before the
// point was there.
//
// WebSearch is plugins/search's: uni-switch's web search for a model that
// can't search by itself. Off, a vendor that searches by itself still
// does; uni-switch offers no search of its own. PluginTools is plugins/tools':
// the tools community plugins give, served to every agent over MCP. Off,
// the server lists none and runs none.
package gwtool

import "sync"

// WebSearch is uni-switch's web search: the stand-in web_search tool, the
// search MCP server and Codex's search when ChatGPT can't answer it.
const WebSearch = "web_search"

// PluginTools are the tools the user's plugins give (their OpenCode tool
// hook), served over uni-switch's plugin tools MCP server.
const PluginTools = "plugin_tools"

var gate struct {
	sync.Mutex
	gated  int            // Gate calls not yet stopped
	served map[string]int // name → Serve calls not yet removed
}

// Gate makes every tool wait on Serve until stop: the gateway calls it as
// it starts serving.
func Gate() (stop func()) {
	gate.Lock()
	gate.gated++
	gate.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			gate.Lock()
			gate.gated--
			gate.Unlock()
		})
	}
}

// Serve serves the tool name until remove: its plugin calls it while it
// is on.
func Serve(name string) (remove func()) {
	gate.Lock()
	if gate.served == nil {
		gate.served = map[string]int{}
	}
	gate.served[name]++
	gate.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			gate.Lock()
			gate.served[name]--
			gate.Unlock()
		})
	}
}

// Served says whether the tool name is offered and answered here now.
func Served(name string) bool {
	gate.Lock()
	defer gate.Unlock()
	return gate.gated == 0 || gate.served[name] > 0
}
