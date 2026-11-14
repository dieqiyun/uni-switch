package provider

import (
	"runtime"
	"testing"
)

// signInLink runs a fake login command printing script and says the link
// the sign-in hands to the window. It is /bin/sh -c, not a script written
// for the test: macOS checks a new executable before its first run, which
// takes ~0.3s alone and seconds while a `go test ./...` starts its other
// new test binaries, all of it counted against linkWait.
func signInLink(t *testing.T, whole func(string) bool, script string) string {
	t.Helper()
	s := &signInFlow{done: make(chan struct{})}
	s.st = SignInState{State: "waiting"}
	err := runCLISignIn(s, "login", nil, true, nil, func() (string, string, bool) { return "", "", false }, whole, "/bin/sh", "-c", script+"\nexec sleep 5")
	s.mu.Lock()
	stop, u := s.stop, s.st.URL
	s.mu.Unlock()
	if stop != nil {
		stop()
		// the sign-in ends with the test: what it does as it ends (it
		// forgets the accounts' caches) would be the next test's
		<-s.done
	}
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// An agent whose link needs nothing more hands on the first one as it was:
// a device code printed on the line after stays off it.
func TestCLISignInFirstLinkAsPrinted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI is a shell script")
	}
	if got := signInLink(t, nil, `printf 'Open https://accounts.x.ai/device\nABCD-EFGH\n'`); got != "https://accounts.x.ai/device" {
		t.Errorf("link %q", got)
	}
}

// 𝕏 on Discord: "grok login gave no link to open", without what the CLI
// said. A login that fails says its last line, and the endpoint its error
// names isn't taken for the page to open.
func TestCLISignInSaysWhyNoLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI is a shell script")
	}
	said := "Error: error sending request for url (https://auth.x.ai/oauth2/device/code): client error (Connect): tunnel error: failed to create underlying connection: tcp connect error: Connection refused (os error 61)"
	s := &signInFlow{done: make(chan struct{})}
	s.st = SignInState{State: "waiting"}
	err := runCLISignIn(s, "grok login", nil, true, nil, func() (string, string, bool) { return "", "", false }, nil, "/bin/sh", "-c", "echo '"+said+"' >&2; exit 1")
	<-s.done // the sign-in ends with the test, as signInLink's does
	if err == nil || err.Error() != "grok login gave no link to open: "+said {
		s.mu.Lock()
		t.Fatalf("err %v, link %q", err, s.st.URL)
	}
}
