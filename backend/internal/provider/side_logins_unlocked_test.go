package provider

import (
	"slices"
	"testing"
	"time"
)

// #1665: sideLogins took loginsMu, and with it made and locked
// logins.json.lock, for every agent's list of accounts, though nearly every
// list changes nothing: ~55 locks in a start's first 20s, each a file an
// antivirus' filter driver works on. A list with nothing to write takes no
// lock: it returns while another holds it; one with a change still writes.
func TestSideLoginsNothingToChangeTakesNoLock(t *testing.T) {
	signIn(t)
	all := func(savedLogin) bool { return true }
	sideLogins("google", "a@example.com", all) // its own sign-in kept: a write

	loginsMu.Lock() // another, writing logins.json
	done := make(chan []sideLogin)
	go func() {
		var got []sideLogin
		for range 100 {
			got = sideLogins("google", "a@example.com", all)
		}
		done <- got
	}()
	var got []sideLogin
	select {
	case got = <-done:
	case <-time.After(5 * time.Second):
		loginsMu.Unlock()
		<-done
		t.Fatal("a list with nothing to change waited for logins.json's lock")
	}
	loginsMu.Unlock()
	if !slices.ContainsFunc(got, func(s sideLogin) bool { return s.User == "a@example.com" }) {
		t.Fatalf("the agent's own account isn't listed: %+v", got)
	}

	// signed in to another account, the change is written, under the lock
	sideLogins("google", "b@example.com", all)
	if !slices.ContainsFunc(readLogins(), func(l savedLogin) bool { return l.Agent == "google" && l.User == "b@example.com" }) {
		t.Fatalf("the agent's new account wasn't written: %+v", readLogins())
	}
}
