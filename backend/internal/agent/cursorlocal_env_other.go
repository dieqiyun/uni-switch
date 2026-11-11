//go:build !darwin && !windows

package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// cursorLocalEnvD is the environment.d file systemd's user session reads
// at login, the variables in it for every app the desktop starts.
func cursorLocalEnvD() string {
	cfg := appdir.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		home, _ := os.UserHomeDir()
		cfg = filepath.Join(home, ".config")
	}
	return filepath.Join(cfg, "environment.d", "uni-switch-cursor-local.conf")
}

// setUserEnv writes the variables to environment.d, for the sessions from
// the next login on, and gives them to the user's systemd now (for apps it
// starts from now on, where it is there to); nil removes them.
func setUserEnv(env map[string]string) error {
	p := cursorLocalEnvD()
	if env == nil {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		if !testing.Testing() {
			proc.Command("systemctl", append([]string{"--user", "unset-environment"}, cursorLocalVars...)...).Run()
		}
		return nil
	}
	var b strings.Builder
	var kv []string
	for _, k := range cursorLocalVars {
		b.WriteString(k + "=" + env[k] + "\n")
		kv = append(kv, k+"="+env[k])
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		return err
	}
	if !testing.Testing() {
		proc.Command("systemctl", append([]string{"--user", "set-environment"}, kv...)...).Run()
	}
	return nil
}
