package provider

import (
	"testing"
	"time"
)

// #1665: rememberLogins took loginsMu, and with it made and locked
// logins.json.lock, before it looked whether 30 seconds had passed since
// its last look: every list of accounts did, ~370 times in a Windows
// start's first 25s, each a file an antivirus' filter driver works on.
// A look that isn't due takes no lock: it returns while another holds it.
func TestRememberLoginsNotDueTakesNoLock(t *testing.T) {
	signIn(t)
	rememberLogins(true) // a look, just now

	loginsMu.Lock() // another, writing logins.json
	done := make(chan struct{})
	go func() {
		for range 100 {
			rememberLogins(false)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		loginsMu.Unlock()
		<-done
		t.Fatal("a look not due waited for logins.json's lock")
	}
	loginsMu.Unlock()

	// due again, it looks, under the lock
	loginsSeenAt.Store(time.Now().Add(-31 * time.Second).UnixNano())
	rememberLogins(false)
	if loginsLookDue() {
		t.Fatal("a due look didn't look")
	}
}
