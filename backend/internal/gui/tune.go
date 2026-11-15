package gui

import (
	"encoding/json"
	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"net/http"
)

// claudeCacheJSON is one of Claude Code's prompt-cache lifetimes: its
// knob (agent.TuneCache, agent.TuneSubCache), the value it runs at ("" for
// Claude Code's own default), and the variable that sets it, when one does.
type claudeCacheJSON struct {
	Knob   string `json:"knob"`
	Value  string `json:"value,omitempty"`
	Locked string `json:"locked,omitempty"`
}

func claudeCacheState() []claudeCacheJSON {
	var out []claudeCacheJSON
	for _, knob := range []string{agent.TuneCache, agent.TuneSubCache} {
		s, err := agent.TuneRead("", "claude", knob, "")
		if err != nil {
			continue
		}
		out = append(out, claudeCacheJSON{Knob: knob, Value: s.Value, Locked: s.Locked})
	}
	return out
}

// claudeCacheRoutes is the retained client setting, independent of reports.
func claudeCacheRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/settings/claude-cache", func(rw http.ResponseWriter, r *http.Request) {
		var in struct{ Knob, Value string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(rw, err)
			return
		}
		if err := agent.ClaudeCacheSet("", in.Knob, in.Value); err != nil {
			fail(rw, err)
			return
		}
		writeJSON(rw, settingsState())
	})
}
