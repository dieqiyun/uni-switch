//go:build !windows

package provider

import (
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// securityKeychain reads `security` as macOS answers: exit 44 is no item,
// a password with a newline comes back as hex, and an error says neither
// the arguments nor the output, which can hold the sign-in. The program is
// a fake on PATH: no test runs the real one.
func TestSecurityKeychainAnswers(t *testing.T) {
	bin := t.TempDir()
	hexed := hex.EncodeToString([]byte("{\"tokens\":{\"access_token\":\"x\"}}\n"))
	script := `#!/bin/sh
case "$1" in
find-generic-password)
  case "$5" in
  "cli|none") exit 44 ;;
  "cli|hex") echo ` + hexed + `; exit 0 ;;
  esac
  echo "denied SECRET-OUT" >&2; exit 51 ;;
add-generic-password) echo "$@" >&2; exit 1 ;;
delete-generic-password) exit 44 ;;
esac
exit 2
`
	// a program macOS has let run already: a new one's first exec waits for
	// its check, past the keychain's wait under a full run's load
	testenv.Program(t, filepath.Join(bin, "security"), script)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	k := securityKeychain{}
	if _, err := k.Find(codexKeychainService, "cli|none"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("exit 44: %v, want no item", err)
	}
	if b, err := k.Find(codexKeychainService, "cli|hex"); err != nil || !strings.HasPrefix(string(b), `{"tokens"`) {
		t.Fatalf("hex: %q %v", b, err)
	}
	if _, err := k.Find(codexKeychainService, "cli|refused"); err == nil || errors.Is(err, fs.ErrNotExist) || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("refused: %v, want an error that isn't no item and says nothing it was given", err)
	}
	if err := k.Put(codexKeychainService, "cli|x", []byte(`{"SECRET-IN":1}`)); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("put: %v", err)
	}
	if err := k.Remove(codexKeychainService, "cli|x"); err != nil {
		t.Fatalf("removing no item: %v", err)
	}
}
