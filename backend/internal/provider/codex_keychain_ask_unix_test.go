//go:build !windows

package provider

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// macOS' `security` as uni-switch runs it: the secret is read with room for
// the user (keychainAsk, not keychainWait), and its exits are told apart
// (SecBase.h: errSecItemNotFound -25300 → 44, errSecAuthFailed -25293 →
// 51, errSecUserCanceled -128 → 128). A fake `security` on PATH; never
// the real one.
func TestSecurityKeychainExits(t *testing.T) {
	bin := t.TempDir()
	script := `#!/bin/sh
case "$FAKE_SECURITY" in
slow) /bin/sleep 2 ;;
hang) exec /bin/sleep 30 ;;
missing) exit 44 ;;
denied) exit 51 ;;
cancelled) exit 128 ;;
broken) exit 1 ;;
esac
for a in "$@"; do
  if [ "$a" = "-w" ]; then echo '{"tokens":{}}'; exit 0; fi
done
echo 'attributes:'
echo '    "mdat"<timedate>=0x32303236313031353138333235375A00  "20261015183257Z\000"'
`
	testenv.Program(t, filepath.Join(bin, "security"), script)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	defer func(w, a time.Duration) { keychainWait, keychainAsk = w, a }(keychainWait, keychainAsk)
	k := securityKeychain{}
	// the user answers after twice the wait a run with no prompt has
	keychainWait, keychainAsk = time.Second, time.Minute
	t.Setenv("FAKE_SECURITY", "slow")
	if b, err := k.Find(codexKeychainService, "cli|x"); err != nil || string(b) != `{"tokens":{}}` {
		t.Fatalf("a read the user answered late: %s %v", b, err)
	}
	keychainWait = time.Minute
	t.Setenv("FAKE_SECURITY", "")
	if d, err := k.Stat(codexKeychainService, "cli|x"); err != nil || !strings.Contains(d, "20261015183257Z") {
		t.Fatalf("Stat: %q %v", d, err)
	}
	for mode, want := range map[string]error{"missing": fs.ErrNotExist, "denied": errKeychainRefused, "cancelled": errKeychainRefused, "broken": errKeychainUnread} {
		t.Setenv("FAKE_SECURITY", mode)
		if _, err := k.Find(codexKeychainService, "cli|x"); !errors.Is(err, want) {
			t.Errorf("Find, %s: %v, want %v", mode, err, want)
		}
	}
	t.Setenv("FAKE_SECURITY", "missing")
	if _, err := k.Stat(codexKeychainService, "cli|x"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Stat, missing: %v", err)
	}
	// no answer at all
	keychainAsk = 300 * time.Millisecond
	t.Setenv("FAKE_SECURITY", "hang")
	if _, err := k.Find(codexKeychainService, "cli|x"); !errors.Is(err, errKeychainNoAnswer) {
		t.Errorf("Find, no answer: %v", err)
	}
}
