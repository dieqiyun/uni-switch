package gateway

import (
	"sync"
	"time"
)

// A publicly reachable gateway turns every request away without a valid
// gateway key, but nothing capped how fast a remote client could try keys:
// no limit let a weak user-set key be guessed, and each try gave a yes/no
// oracle on whether a key was valid. authFailThrottle caps *failed remote*
// authentications. A request that carries a valid key, and any request from
// this computer, never touch it, so flooding wrong keys can only turn later
// wrong keys into 429s — it can never lock a real user out.
//
// It is one token bucket shared across all remote callers. Keying it per
// client address would be pointless behind a reverse proxy, where every
// caller arrives as the proxy's address; a shared bucket is safe here only
// because valid keys bypass it entirely.

const (
	authFailBurst  = 50              // wrong remote keys tolerated before 429s begin
	authFailRefill = 1 * time.Second // one more wrong key is allowed per this
)

var authThrottle = struct {
	sync.Mutex
	tokens float64
	last   time.Time
	now    func() time.Time
}{tokens: authFailBurst, now: time.Now}

// tooManyAuthFails records one refused remote authentication and reports
// whether the caller should now be turned away with 429 instead of the usual
// 401, and for how long until a token frees. Only identifyCaller calls it,
// and only for a remote request that presented no valid key.
func tooManyAuthFails() (over bool, retry time.Duration) {
	t := &authThrottle
	t.Lock()
	defer t.Unlock()
	now := t.now()
	if t.last.IsZero() {
		t.last = now
	}
	t.tokens += now.Sub(t.last).Seconds() / authFailRefill.Seconds()
	if t.tokens > authFailBurst {
		t.tokens = authFailBurst
	}
	t.last = now
	if t.tokens >= 1 {
		t.tokens--
		return false, 0
	}
	// No token left: say how long until one refills, so the client can back
	// off. The failure is not charged again while the bucket is empty.
	return true, time.Duration((1 - t.tokens) * float64(authFailRefill))
}

// resetAuthThrottle refills the bucket. fresh(t) calls it so the global
// counter never carries wrong-key tries from one test into the next.
func resetAuthThrottle() {
	t := &authThrottle
	t.Lock()
	t.tokens, t.last, t.now = authFailBurst, time.Time{}, time.Now
	t.Unlock()
}
