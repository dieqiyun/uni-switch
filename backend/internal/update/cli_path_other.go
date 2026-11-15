//go:build !windows

package update

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// installedShells are the shells installed here that a terminal may open:
// zsh, bash and fish, the login shell ($SHELL) marked, and that one too
// when it is another (nu, sh), with no profile uni-switch writes.
func installedShells() []cliShell {
	login := os.Getenv("SHELL")
	var out []cliShell
	for _, n := range []string{"zsh", "bash", "fish"} {
		p := proc.FindShell(n)
		if p == "" {
			continue
		}
		out = append(out, cliShell{Name: n, Path: p, Default: filepath.Base(login) == n})
	}
	if login != "" && filepath.IsAbs(login) && !slices.ContainsFunc(out, func(s cliShell) bool { return s.Default }) {
		out = append([]cliShell{{Name: filepath.Base(login), Path: login, Default: true}}, out...)
	}
	return out
}

// cliDir is the folder a link to exe goes in for a shell with PATH dirs:
// where the uni-switch it runs now (cmd) is, when that is another copy in a
// folder uni-switch can write — the link takes its place, as install.sh's
// ln -sf does — else the first of the folders a user's commands usually go
// in that is on it and can be written to; "" when none is.
func cliDir(exe string, dirs []string, cmd string) string {
	if cmd != "" {
		if d := filepath.Dir(cmd); Writable(d) {
			return d
		}
		return ""
	}
	var cands []string
	if h := os.Getenv("HOME"); filepath.IsAbs(h) {
		cands = append(cands, filepath.Join(h, ".local", "bin"), filepath.Join(h, "bin"))
	}
	home := len(cands)
	cands = append(cands, "/opt/homebrew/bin", "/usr/local/bin", "/home/linuxbrew/.linuxbrew/bin")
	for i, d := range cands {
		if !slices.Contains(dirs, d) {
			continue
		}
		st, err := os.Stat(d)
		if err == nil && st.IsDir() && Writable(d) {
			return d
		}
		// on PATH but not made yet: the home's are made
		if i < home && os.IsNotExist(err) {
			return d
		}
	}
	return ""
}

// cliPlace links exe as uni-switch in dir, over what is there.
func cliPlace(exe, dir string) error {
	dst := filepath.Join(dir, "uni-switch")
	if sameFile(dst, exe) {
		return nil
	}
	if st, err := os.Lstat(dst); err == nil && st.IsDir() {
		return fmt.Errorf("%s is a folder", dst)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := dst + ".uni-switch-new"
	os.Remove(tmp)
	if err := os.Symlink(exe, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
