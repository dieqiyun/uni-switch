package provider

// A keychain read that can have shown the user a dialog and was refused is
// not asked again every poll: copilotCLISecret backs one off, and so do
// the Cursor and Claude Code readers (#2408). A fake `security` answers
// the way a refused read does (51, not 44, the item not being there), and
// logs each ask; the reader may reach it once, and then not again inside
// the denial's pause.

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

func fakeSecurityDenied(t *testing.T) func() int {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "asked.log")
	testenv.Program(t, filepath.Join(dir, "security"), "#!/bin/sh\necho \"$*\" >> "+log+"\nexit 51\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() int {
		b, _ := os.ReadFile(log)
		return bytes.Count(b, []byte("\n"))
	}
}

func TestDeniedCursorKeychainReadIsNotAskedAgain(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the Cursor keychain read is a macOS one")
	}
	signIn(t)
	asks := fakeSecurityDenied(t)
	cursorTok.Lock()
	cursorTok.tok, cursorTok.at = "", time.Time{}
	cursorTok.Unlock()
	if tok := cursorKeychainToken(false); tok != "" {
		t.Fatalf("a refused read is no token: %q", tok)
	}
	if n := asks(); n != 1 {
		t.Fatalf("the first read asks the keychain once, asked %d times", n)
	}
	// past the 10s token TTL, still inside the denial's pause: not asked again
	cursorTok.Lock()
	cursorTok.at = time.Now().Add(-11 * time.Second)
	cursorTok.Unlock()
	if tok := cursorKeychainToken(false); tok != "" {
		t.Fatalf("a refused read is no token: %q", tok)
	}
	if n := asks(); n != 1 {
		t.Fatalf("a refused read was asked again 11s later: %d asks", n)
	}
}

func TestDeniedClaudeKeychainReadIsNotAskedAgain(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the Claude keychain read is a macOS one")
	}
	signIn(t)
	asks := fakeSecurityDenied(t)
	oldKeychain := claudeKeychain
	claudeKeychain = true
	defer func() { claudeKeychain = oldKeychain }()
	forgetClaudeCredential()
	if _, _, ok := claudeCredential(); ok {
		t.Fatal("a refused read is no sign-in")
	}
	n := asks()
	if n == 0 {
		t.Fatal("the read never reached the keychain")
	}
	// past the 10s cache, still inside the denial's pause: not asked again
	claudeCacheMu.Lock()
	claudeCacheAt = time.Now().Add(-11 * time.Second)
	claudeCacheMu.Unlock()
	if _, _, ok := claudeCredential(); ok {
		t.Fatal("a refused read is no sign-in")
	}
	if got := asks(); got != n {
		t.Fatalf("a refused read was asked again 11s later: %d asks, then %d", n, got)
	}
}
