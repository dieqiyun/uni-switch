package daemon

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

// systemd runs the service as a user unit: systemctl --user, no root.
type systemd struct{}

const unit = Name + ".service"

func (systemd) file() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "systemd", "user", unit)
}

func (systemd) logs() string { return "journalctl --user -u " + Name + " -f" }

// unitText is the unit running exe with args, with env set in it.
func unitText(exe string, args, env []string) string {
	var b strings.Builder
	b.WriteString("# Written by `uni-switch daemon install`; `uni-switch daemon uninstall` removes it.\n")
	b.WriteString("[Unit]\nDescription=uni-switch gateway\nWants=network-online.target\nAfter=network-online.target\n\n")
	b.WriteString("[Service]\nType=simple\n")
	b.WriteString("ExecStart=" + unitQuote(exe))
	for _, a := range args {
		if a == "serve" {
			b.WriteString(" serve")
		} else {
			b.WriteString(" " + unitQuote(a))
		}
	}
	b.WriteString("\n")
	b.WriteString("Restart=on-failure\nRestartSec=5\n")
	for _, kv := range env {
		b.WriteString("Environment=" + unitQuote(kv) + "\n")
	}
	b.WriteString("\n[Install]\nWantedBy=default.target\n")
	return b.String()
}

// unitQuote quotes s as one word of a unit file: systemd reads \ and "
// escaped inside quotes, and % as the start of a specifier.
func unitQuote(s string) string {
	s = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%", "\n", " ").Replace(s)
	return `"` + s + `"`
}

func (s systemd) install(exe string, args, env []string) error {
	if err := s.reachable(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.file()), 0o755); err != nil {
		return err
	}
	if err := private(s.file(), []byte(unitText(exe, args, env))); err != nil {
		return err
	}
	if _, err := systemctl("daemon-reload"); err != nil {
		return err
	}
	if _, err := systemctl("enable", unit); err != nil {
		return err
	}
	// restart, not start: an install over one running takes the new unit
	_, err := systemctl("restart", unit)
	return err
}

func (s systemd) uninstall() error {
	if _, err := os.Stat(s.file()); err != nil {
		return nil
	}
	if err := s.reachable(); err != nil {
		return err
	}
	if _, err := systemctl("disable", "--now", unit); err != nil {
		return err
	}
	if err := os.Remove(s.file()); err != nil && !os.IsNotExist(err) {
		return err
	}
	_, err := systemctl("daemon-reload")
	return err
}

func (systemd) start() error   { _, err := systemctl("start", unit); return err }
func (systemd) stop() error    { _, err := systemctl("stop", unit); return err }
func (systemd) restart() error { _, err := systemctl("restart", unit); return err }

func (s systemd) status() (Status, error) {
	if err := s.reachable(); err != nil {
		return Status{}, err
	}
	out, err := systemctl("show", unit, "--property=ActiveState,SubState,MainPID,UnitFileState,ExecMainStatus")
	if err != nil {
		return Status{}, err
	}
	v := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, val, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			v[k] = val
		}
	}
	st := Status{
		Enabled: v["UnitFileState"] == "enabled",
		Running: v["ActiveState"] == "active",
		State:   v["ActiveState"],
	}
	if sub := v["SubState"]; sub != "" && sub != st.State {
		st.State += " (" + sub + ")"
	}
	if pid, err := strconv.Atoi(v["MainPID"]); err == nil && pid > 0 {
		st.PID = pid
	}
	// a stop ends it with SIGTERM's 15: only a run that failed is news
	failing := st.State != "" && (v["ActiveState"] == "failed" || v["ActiveState"] == "activating")
	if code := v["ExecMainStatus"]; failing && code != "" && code != "0" {
		st.LastExit = code
	}
	if a := lingerAdvice(); a != "" {
		st.Advice = append(st.Advice, a)
	}
	return st, nil
}

// reachable says what to do when systemctl --user can't be used: no
// systemd, or a session without the user's bus (su, sudo, some SSH).
func (systemd) reachable() error {
	_, err := systemctl("is-system-running")
	if err == nil {
		return nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		advice := "this system has no systemd (systemctl isn't on PATH): run `uni-switch serve` under the service manager it has"
		if wsl() {
			advice += "; on WSL, turn systemd on with [boot] systemd=true in /etc/wsl.conf, then `wsl --shutdown`"
		}
		return errors.New(advice)
	}
	msg := err.Error()
	if strings.Contains(msg, "bus") || strings.Contains(msg, "XDG_RUNTIME_DIR") {
		return fmt.Errorf("systemctl --user can't reach your session's service manager: log in as this user directly (not through su or sudo), or set XDG_RUNTIME_DIR=/run/user/%d (%s)", os.Getuid(), msg)
	}
	// is-system-running exits non-zero for "degraded" too, which still
	// runs user units
	return nil
}

func wsl() bool {
	b, _ := os.ReadFile("/proc/version")
	return strings.Contains(strings.ToLower(string(b)), "microsoft")
}

// lingerAdvice is what to run so the service starts at boot and keeps
// running after logging out, when it doesn't: systemd starts a user's
// units with their first login and stops them with their last, unless the
// user lingers. Empty when it does, or when loginctl can't tell.
func lingerAdvice() string {
	name := os.Getenv("USER")
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username
	}
	if name == "" {
		return ""
	}
	out, err := run("loginctl", "show-user", name, "--property=Linger", "--value")
	if err != nil || strings.TrimSpace(out) != "no" {
		return ""
	}
	return "it starts when you log in and stops when you log out; to run it from boot, with no one logged in: sudo loginctl enable-linger " + name
}

// Linger is lingerAdvice, for after an install.
func Linger() string {
	if goos != "linux" {
		return ""
	}
	return lingerAdvice()
}

func systemctl(args ...string) (string, error) {
	return run("systemctl", append([]string{"--user"}, args...)...)
}
