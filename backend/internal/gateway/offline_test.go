package gateway

import (
	"time"
)

// forgetNetwork forgets what was heard of the network and the verdict kept.
func forgetNetwork() {
	lastHeard.Store(0)
	offlineSeen.Lock()
	offlineSeen.at, offlineSeen.off = time.Time{}, false
	offlineSeen.Unlock()
}
