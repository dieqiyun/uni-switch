package gui

import (
	"github.com/dieqiyun/uni-switch/backend/internal/pluginrt"
)

// Plugins › Built-in (Lxxyx on X: "功能太多了"): uni-switch's built-in plugins
// (internal/pluginrt, plugins/), each with a switch kept in plugins.yml.
// Off, a plugin's parts leave the window: its page leaves the sidebar and
// the header's tabs, its sections leave the pages they are on, its rows
// leave Settings and Settings' search, and a link to it opens nothing. The
// page hides whatever is marked data-feature="<id>" while the plugin isn't
// active.
//
// One in use stays on: its switch can't be turned off, and its row says
// what uses it. Every install, a new one too, has them all on until the
// user says otherwise.
// TestBuiltinsTranslated asks each string of every plugin of every
// language.

// builtinJSON is a built-in plugin as the page is told it: whether it
// shows (On: active, which plugins.yml or its use makes it), the user's
// own choice, and what of it is in use.
type builtinJSON struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Sub    string          `json:"sub"`
	On     bool            `json:"on"`
	Chosen bool            `json:"chosen"`
	Source pluginrt.Source `json:"source"`
	State  pluginrt.State  `json:"state"`
	// Why is the reason it isn't running, when it is on and isn't
	Why   string          `json:"why,omitempty"`
	InUse int             `json:"inUse"`
	Use   string          `json:"use,omitempty"` // One or Many, by InUse
	Parts []pluginrt.Part `json:"parts,omitempty"`
	// Schema is its settings' (pluginrt/schema.go), which the page draws
	// a form of; Config their values now
	Schema *pluginrt.Schema `json:"schema,omitempty"`
	Config map[string]any   `json:"config,omitempty"`
	// Broken is plugins.yml's error when it can't be read: every
	// plugin runs as it did, and no switch can be changed
	Broken string `json:"broken,omitempty"`
}

// builtinsState composes plugins.yml again, brings the runtime to it, and
// says every built-in plugin, in their order.
func builtinsState() []builtinJSON {
	cs, err := pluginrt.Refresh()
	byID := map[string]pluginrt.Composed{}
	for _, c := range cs {
		byID[c.ID] = c
	}
	st := map[string]pluginrt.Status{}
	if rt := pluginrt.Running(); rt != nil {
		for _, s := range rt.Statuses() {
			st[s.ID] = s
		}
	}
	out := []builtinJSON{}
	for _, m := range pluginrt.Registered() {
		c, s := byID[m.ID], st[m.ID]
		j := builtinJSON{ID: m.ID, Name: m.Name, Sub: m.Sub, On: s.State == pluginrt.Active, Chosen: c.Chosen, Source: c.Source, State: s.State, Why: s.Why, InUse: c.InUse, Parts: s.Parts, Schema: m.Schema(), Config: c.Config}
		if err != nil {
			j.Broken, j.Chosen = err.Error(), j.On
		}
		switch {
		case j.InUse == 1:
			j.Use = m.One
		case j.InUse > 1:
			j.Use = m.Many
		}
		out = append(out, j)
	}
	return out
}

// builtinsOff are the ids of the plugins the window leaves out now, for
// boot.js to hide them before the page paints.
func builtinsOff(list []builtinJSON) []string {
	out := []string{}
	for _, b := range list {
		if !b.On {
			out = append(out, b.ID)
		}
	}
	return out
}

// builtinConfig is each plugin's settings the window shows, by id, for
// boot.js to hand the page's plugins before they draw.
func builtinConfig(list []builtinJSON) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, b := range list {
		if b.On && b.Schema != nil {
			out[b.ID] = b.Config
		}
	}
	return out
}
