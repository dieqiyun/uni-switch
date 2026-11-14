package usage

import (
	"maps"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// clientMap is the user's ClientAgents (#1958) as last read, and a key
// for the caches built on what it counts.
type clientMap struct {
	m   map[string]string
	key string
}

var clientAs atomic.Pointer[clientMap]

// readClientAgents reads the user's ClientAgents again, for a pass over
// the ledger (readLogSnapshot) and the caches' keys: each record of the
// pass is then counted by what was read here.
func readClientAgents() *clientMap {
	m := settings.ClientAgentsNow()
	if cur := clientAs.Load(); cur != nil && maps.Equal(cur.m, m) {
		return cur
	}
	var b strings.Builder
	for _, k := range slices.Sorted(maps.Keys(m)) {
		b.WriteString(k + "=" + m[k] + ";")
	}
	c := &clientMap{m: m, key: b.String()}
	clientAs.Store(c)
	return c
}

// clientAgentsKey is what the caches of counted records key on.
func clientAgentsKey() string { return readClientAgents().key }

// ReportedAs is the agent the Usage and Sessions pages count a client's
// calls under: AgentOf's, unless the user counts that client as an agent
// of theirs (settings.ClientAgents). Only for what is reported: the
// gateway's own choices (routing, keys, caps) go by AgentOf, the client's
// own name.
func ReportedAs(ua string) string {
	a, _ := reportedAs(ua)
	return a
}

// reportedAs is ReportedAs, and the client's own name when the user's
// counting changed it ("" when it didn't).
func reportedAs(ua string) (agent, client string) {
	a := AgentOf(ua)
	c := clientAs.Load()
	if c == nil || len(c.m) == 0 {
		return a, ""
	}
	if to := c.m[strings.ToLower(a)]; to != "" && to != a {
		return to, a
	}
	return a, ""
}
