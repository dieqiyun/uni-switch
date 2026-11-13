// Package sessions is the Sessions page: the agents' past conversations,
// to read, search, resume and export. Off, the page leaves the window;
// the gateway still records conversations while Settings asks it to, and
// that keeps the plugin on.
package sessions

import (
	"github.com/dieqiyun/uni-switch/backend/internal/pluginrt"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

func init() {
	pluginrt.Register(pluginrt.Manifest{
		ID: "sessions", API: "^1", Requires: []string{"window"},
		Name: "Sessions", Sub: "Your agents' past conversations: read, search, resume and export them",
		One: "Recording gateway conversations", Many: "Recording gateway conversations",
		InUse: func() (int, error) {
			s, err := settings.LoadChecked()
			if err != nil {
				return 0, err
			}
			if s.GatewayConversations {
				return 1, nil
			}
			return 0, nil
		},
		Parts:  []pluginrt.Part{{Slot: "nav.page", Key: "sessions"}},
		Client: []string{"i18n.js", "sessions.css", "client.js"},
	})
}
