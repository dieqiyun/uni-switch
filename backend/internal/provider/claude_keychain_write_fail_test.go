package provider

// A Claude sign-in whose Keychain write fails (mote over ssh: the login
// keychain out of reach, #4563) leaves logins.json alone: the account
// isn't kept when its credential couldn't be stored, or importing the
// same file again would report it as already there.

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

func TestClaudeLoginKeychainWriteFailureKeepsLoginsFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for the keychain")
	}
	signIn(t) // a temp home: no Claude sign-in in it, no saved logins
	bin := t.TempDir()
	// a refused keychain: no item there (51), and no write kept
	testenv.Program(t, filepath.Join(bin, "security"), "#!/bin/sh\nexit 51\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	oldKeychain := claudeKeychain
	claudeKeychain = true
	defer func() { claudeKeychain = oldKeychain }()

	l, err := claudeLogin(claudeCredentials{OAuth: claudeAuth{
		AccessToken: "a", RefreshToken: "r",
		ExpiresAt: time.Now().Add(time.Hour).UnixMilli(), SubscriptionType: "max",
	}}, map[string]any{"emailAddress": "ssh@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(loginsPath())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := addLogin(l); err == nil {
		t.Fatal("a sign-in whose keychain write failed was kept")
	}
	after, err := os.ReadFile(loginsPath())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("logins.json changed on a failed keychain write: %s", after)
	}
}
