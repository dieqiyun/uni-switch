package settings

import (
	"os"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/filememo"
)

// AccountDiscoveryOff is whether uni-switch leaves agents' sign-ins unread
// (#1535): NoAccountDiscovery saved, or UNI_SWITCH_NO_ACCOUNTS set to anything
// but "", "0" or "false" for this process alone. The variable only ever
// turns discovery off; it never turns on what the user saved off.
func AccountDiscoveryOff() bool {
	if noAccountsEnv() {
		return true
	}
	fileMu.RLock()
	defer fileMu.RUnlock()
	// the one setting, parsed once for each version of the file: it is
	// asked for each agent and each plugin every time the providers are
	// built, a whole parse of settings.json each (#2502)
	off, err := filememo.Read("settings:no-accounts", Path(), func(b []byte) (bool, error) {
		s, err := parse(b)
		return s.NoAccountDiscovery, err
	})
	if err != nil {
		return load().NoAccountDiscovery
	}
	return off
}

// AccountDiscoveryOffIn is AccountDiscoveryOff with the settings already
// read, for a caller asking it many times in one look.
func AccountDiscoveryOffIn(s Settings) bool {
	return noAccountsEnv() || s.NoAccountDiscovery
}

func noAccountsEnv() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("UNI_SWITCH_NO_ACCOUNTS")))
	return v != "" && v != "0" && v != "false"
}
