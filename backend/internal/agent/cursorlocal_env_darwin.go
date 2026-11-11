package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// setUserEnv sets the variables in launchd's user session, which apps
// opened from the Dock or Finder from now on start with; nil unsets them.
// launchd forgets them at a restart, when uni-switch sets them again.
func setUserEnv(env map[string]string) error {
	if testing.Testing() {
		return nil
	}
	for _, k := range cursorLocalVars {
		args := []string{"unsetenv", k}
		if v, ok := env[k]; ok {
			args = []string{"setenv", k, v}
		}
		if out, err := proc.Command("launchctl", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("launchctl %s: %v %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}
