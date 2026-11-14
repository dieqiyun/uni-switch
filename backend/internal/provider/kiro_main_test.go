package provider

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// TestMain keeps the package's tests off the Kiro and Copilot CLI sign-ins
// of whoever runs them: Accounts() reads them, and asks Kiro who the account is. It also
// gives the package a home of its own (testenv), so what a test leaves
// behind it — a CLI's answer kept by refresh after the test is over —
// never lands in the runner's uni-switch, nor in APPDATA on Windows. And it
// keeps them off the network (testenv.Offline): a test of a sign-in or a
// key at a vendor's real address asks only what it serves on loopback.
func TestMain(m *testing.M) {
	os.Exit(testenv.RunIn(m, func(home string) {
		testenv.Offline()
		kiroCLIDB = func() string { return filepath.Join(home, "kiro-test", "kiro-cli", "data.sqlite3") }
		kiroIDEDir = func() string { return filepath.Join(home, "kiro-test", "sso") }
		askKiroIdentity = func(string, string) (string, string) { return "", "" }
		// never the runner's keychain: a test that wants the Copilot
		// CLI's token there stands one in itself
		copilotCLISecret = func(string) string { return "" }
		// the fakes of ChatGPT's backend read turns plain; codex_zstd's
		// tests send them as the real one is sent
		CodexZstdForTests(false)
		// the fakes' device sign-ins give an interval of 0: polled at
		// once, not a second apart (#2051)
		devicePollFloor = 10 * time.Millisecond
	}))
}
