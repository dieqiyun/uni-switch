package usage

import (
	"github.com/dieqiyun/uni-switch/backend/internal/plugin"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// KindKeepalive is the Kind of a cache keepalive ping a plugin sent on its
// own (Devin's, plugins#592): the conversation's request sent again with
// max_tokens 1 so the vendor's prompt cache outlives its 5 minutes.
const KindKeepalive = "cache_keepalive"

// pluginCall is the row of a request a plugin reported it sent its vendor
// on its own (plugin.OwnCall, uni-switch.usage in host.js): uni-switch's, as the
// routing classifier's are, by the plugin's provider and the account it
// was sent for, labelled by its Kind. It is counted as the gateway's
// calls are, in the totals and the cost: such a ping is billed (a cache
// read of the whole context), and the user who turned it on is to see
// what it costs (zelong77, plugins#592).
func pluginCall(id, account string, u plugin.OwnCall) Record {
	return Record{Agent: "uni-switch", Provider: provider.PluginID(id), ProviderAccount: provider.PluginAccountUser(id, account),
		Model: u.Model, Requested: u.Model, Kind: u.Kind, Input: u.Input, Output: u.Output, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite,
		Status: u.Status, Millis: u.Millis, Error: clipProbeError(u.Error)}
}

func init() {
	plugin.OnUsage(func(id, account string, u plugin.OwnCall) { Append(pluginCall(id, account, u)) })
}
