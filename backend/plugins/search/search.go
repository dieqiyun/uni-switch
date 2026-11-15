// Package search is uni-switch's web search for a model that can't search by
// itself: the agent's web_search tool, answered by a provider that can
// search or a search API set up in Settings. While a search API is set up
// or a searcher is picked, it stays on.
//
// It serves gwtool.WebSearch on the gateway's tool point (P3 of
// docs/subsystems/plugin-runtime.md). Off, uni-switch offers and answers no
// search of its own: the stand-in tool, the search MCP server and Codex's
// search in ChatGPT's place say there is no searcher, and a vendor that
// searches by itself still does.
package search

import (
	"github.com/dieqiyun/uni-switch/backend/internal/gwtool"
	"github.com/dieqiyun/uni-switch/backend/internal/pluginrt"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

func init() {
	pluginrt.Register(pluginrt.Manifest{
		ID: "search", API: "^1", Requires: []string{"settings", "gateway"}, Essential: true,
		Name: "Web search", Sub: "Models that can't search the web search with a provider that can, or a search API",
		One: "1 search source set up", Many: "{n} search sources set up",
		InUse: InUse,
		Apply: func(c *pluginrt.Ctx) error {
			c.OnDispose(gwtool.Serve(gwtool.WebSearch))
			return nil
		},
	})
}

// InUse counts the search APIs set up, and a searcher picked in Settings
// (neither uni-switch's pick nor off); a providers.json or settings that can't
// be read are not known, so it stays on.
func InUse() (int, error) {
	n, err := provider.SearchAPIsSet()
	if err != nil {
		return 0, err
	}
	st, err := settings.LoadChecked()
	if err != nil {
		return 0, err
	}
	if st.Searcher != "" && st.Searcher != "off" {
		n++
	}
	return n, nil
}
