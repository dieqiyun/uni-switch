package gateway

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync/atomic"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// RestartPath is where `uni-switch update`, once it has put a new version in,
// asks the uni-switch serving the gateway to restart into it
// (gaubeebangeel on X: an agent served by uni-switch runs the update at 4am,
// and the app stayed on the old version until someone quit and reopened
// it, which cut whatever the agents had in flight). The app restarts the
// way its own update does: on a Mac or Linux it hands the gateway to the
// new version, so the request that ran the update, and every other,
// goes on. Loopback only, from a program, never a page.
const RestartPath = "/v1/uni-switch/restart"

// RestartReply is what RestartPath answers.
type RestartReply struct {
	// restarting: at once; waiting: once what a restart would cut short
	// (Busy) is done; current: this uni-switch wasn't the one updated (another
	// copy, or nothing put in); unsupported: this uni-switch (uni-switch serve)
	// doesn't restart itself, and is restarted the way it was started
	State string `json:"state"`
	// Version is this uni-switch's, the one restarting
	Version string `json:"version"`
	// Handover: the new version takes the gateway over while this one
	// finishes its requests (Busy is then the Claude subscription's turns)
	Handover bool  `json:"handover,omitempty"`
	Busy     *Busy `json:"busy,omitempty"`
}

var restarter atomic.Pointer[func(version string) RestartReply]

// SetRestarter has RestartPath call f with the version `uni-switch update`
// put in; the gui's updater sets it. Without one the answer is
// "unsupported".
func SetRestarter(f func(version string) RestartReply) {
	if f == nil {
		restarter.Store(nil)
		return
	}
	restarter.Store(&f)
}

// restartRefused says why r may not restart this uni-switch, "" when it may:
// as heapRefused, only a program on this computer calling localhost or a
// loopback address, never another machine, a page, or another Host.
func restartRefused(r *http.Request) string {
	switch {
	case !local(r):
		return "uni-switch restarts to update only when a program on this computer asks, over loopback: run uni-switch update there"
	case fromBrowser(r):
		return "uni-switch doesn't restart when a web page asks: run uni-switch update in a terminal"
	case !loopbackHost(r.Host):
		return "uni-switch restarts to update only on a call to localhost or a loopback address, not " + r.Host + ": run uni-switch update"
	}
	return ""
}

func (s *Server) restartToUpdate(w http.ResponseWriter, r *http.Request) {
	if why := restartRefused(r); why != "" {
		log.Printf("refused %s %s from %s: %s", r.Method, r.URL.Path, r.RemoteAddr, why)
		writeError(w, provider.Chat, http.StatusForbidden, why)
		return
	}
	var in struct {
		Version string `json:"version"`
	}
	json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in)
	reply := RestartReply{State: "unsupported", Version: Version}
	if f := restarter.Load(); f != nil {
		reply = (*f)(in.Version)
		reply.Version = Version
	}
	log.Printf("restart to update to %q asked from %s: %s", in.Version, r.RemoteAddr, reply.State)
	writeJSON(w, http.StatusOK, reply)
}
