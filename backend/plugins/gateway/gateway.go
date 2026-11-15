// Package gateway registers the local gateway settings module.
// Status, keys, connection examples and request records live in Settings.
package gateway

import (
	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/pluginrt"
)

// Settings holds the request-record pagination default.
type Settings struct {
	CallsPage int `json:"callsPage" title:"Recent calls at a time" description:"How many of the gateway's recent calls the page shows, and how many more each Show more adds" enum:"10,20,50,100"`
}

func init() {
	pluginrt.Register(pluginrt.Manifest{
		ID: "gateway", API: "^1", Requires: []string{"window", "gateway"}, Essential: true,
		Name: "Gateway settings", Sub: "Local gateway status, request records and advanced connection settings",
		One: "1 gateway key", Many: "{n} gateway keys",
		InUse: func() (int, error) {
			keys, err := access.List()
			return len(keys), err
		},
		Settings: Settings{CallsPage: 20},
		Client:   []string{"i18n.js", "keys.js", "client.js"},
	})
}
