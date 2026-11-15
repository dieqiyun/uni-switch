package gui

import (
	"log"
	"strings"
	"sync/atomic"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/update"
)

// A uni-switch of another version keeping the gateway's port sends every
// agent's request its own way, while this one writes the agents' settings
// (matrix0487 on Discord: the app wrote pi a model its older gateway
// didn't know, and pi said 404 "add a provider in uni-switch first"). Before,
// this uni-switch only logged it and waited for that one to quit, and only
// the Gateway page said so. Now:
//
//   - an older copy of the app (it says it serves as "app" or "tray", a
//     released version both) is this app's own predecessor: this one
//     takes the gateway over from it once it has no request in flight
//     (gateway.ServedBusy), quitting it as the Gateway page's button does
//     (quitUniSwitchOnPort: only a uni-switch of this user's, by its program),
//     so no request is cut off; looked at again every gatewayWatch;
//   - any other (uni-switch serve, a service, uni-switch web, a TUI, a newer
//     uni-switch, a build of its own) is someone's to quit or update: it is
//     left running and the Agents page says so, with what to do
//     (gatewayOther, in /api/state).

// gatewaySkew is a uni-switch of another version serving the gateway, as the
// page is told it.
type gatewaySkew struct {
	Version string `json:"version"` // the other's
	Mine    string `json:"mine"`    // this uni-switch's
	Serves  string `json:"serves,omitempty"`
	Pid     int    `json:"pid,omitempty"`
	// Older: it is older than this one; Newer: newer; neither, a build
	// the two can't be ordered by (one of its own)
	Older bool `json:"older,omitempty"`
	Newer bool `json:"newer,omitempty"`
	// Replacing: this app takes the gateway over from it once it is idle;
	// Busy is what it has in flight meanwhile, -1 when it doesn't say
	Replacing bool `json:"replacing,omitempty"`
	Busy      int  `json:"busy,omitempty"`
	// Stuck: why quitting it didn't go (gatewayFixJSON's Reason); this
	// one stops trying, and the page offers the button instead
	Stuck string `json:"stuck,omitempty"`
}

// gatewayOther is the uni-switch of another version serving the gateway, nil
// while none does (this one serves it, or one of the same version).
var gatewayOther atomic.Pointer[gatewaySkew]

// replacedOlder counts the older apps this one has quit to serve the
// gateway; past replaceMost something starts them again (a login item
// pointing at an old copy), and they are left to the user.
var replacedOlder atomic.Int32

const replaceMost = 3

// bare is a version without the v a tag has.
func bare(v string) string { return strings.TrimPrefix(v, "v") }

// skewOf is how the uni-switch o serving the gateway differs from this one,
// nil when it doesn't (the same version, or one that doesn't say).
func skewOf(o gateway.Served) *gatewaySkew {
	mine := gateway.Version
	if o.Version == "" || bare(o.Version) == bare(mine) || !update.Released(mine) {
		// a build of this uni-switch's own (make dev) beside the installed
		// one is its developer's to know about
		return nil
	}
	return &gatewaySkew{
		Version: bare(o.Version), Mine: bare(mine), Serves: o.Serves, Pid: o.Pid,
		Older: update.Newer(mine, o.Version), Newer: update.Newer(o.Version, mine),
	}
}

// replaceable is whether this uni-switch takes the gateway over from o by
// itself: this is the app, and o an older release of it.
func replaceable(o gateway.Served, sk *gatewaySkew) bool {
	return handoverHost.Load() && sk != nil && sk.Older && update.Released(o.Version) &&
		(o.Serves == "app" || o.Serves == "tray") && o.Pid > 0 &&
		replacedOlder.Load() < replaceMost
}

// otherServes, under gatewayMu with this one serving nothing, looks at the
// uni-switch o that serves the gateway: what the page is told of it, and, for
// an older copy of the app with no request in flight, quits it. True when
// it was quit and the port is free for this one.
func otherServes(o gateway.Served) bool {
	sk := skewOf(o)
	before := gatewayOther.Load()
	if sk == nil {
		gatewayOther.Store(nil)
		return false
	}
	if before != nil && before.Stuck != "" && before.Pid == sk.Pid {
		sk.Stuck = before.Stuck // tried; the button is the user's now
	} else if replaceable(o, sk) {
		sk.Replacing = true
		n, ok := gateway.ServedBusy()
		if !ok {
			n = -1
		}
		sk.Busy = n
		if n == 0 {
			log.Printf("gateway: uni-switch %s (pid %d), an older copy of this app, serves %s with nothing in flight; quitting it for this one (%s) to serve", sk.Version, sk.Pid, gateway.URL(), sk.Mine)
			out := quitUniSwitchOnPort(o.Version)
			if out.OK {
				replacedOlder.Add(1)
				gatewayOther.Store(nil)
				return true
			}
			log.Printf("gateway: uni-switch %s wasn't quit (%s %s); it keeps the gateway", sk.Version, out.Reason, out.Error)
			sk.Replacing, sk.Busy, sk.Stuck = false, 0, out.Reason
		}
	}
	if before == nil || before.Version != sk.Version || before.Pid != sk.Pid {
		how := "older than this one"
		switch {
		case sk.Newer:
			how = "newer than this one"
		case !sk.Older:
			how = "a build other than this one"
		}
		next := "until it quits"
		if sk.Replacing {
			next = "until this one takes it over, once it has no request in flight"
		}
		log.Printf("gateway: uni-switch %s (%s, pid %d) serves %s, %s (%s); agents' requests go through it %s", sk.Version, sk.Serves, sk.Pid, gateway.URL(), how, sk.Mine, next)
	}
	gatewayOther.Store(sk)
	return false
}
