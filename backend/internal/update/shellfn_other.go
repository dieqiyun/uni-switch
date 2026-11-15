//go:build !windows

package update

import (
	"os"
	"path/filepath"
	"runtime"
)

// shellProfiles are the profiles of the shells installed here that a
// function goes in: zsh's, bash's and fish's (cliProfile), the one a
// terminal opens made when it isn't there yet. A Mac's login bash reads
// the first of .bash_profile, .bash_login and .profile there is.
func shellProfiles() []ShellProfile {
	var out []ShellProfile
	for _, sh := range cliShells() {
		p := cliProfile(sh.Name)
		if p == "" {
			continue
		}
		kind := "posix"
		create := sh.Default
		switch sh.Name {
		case "fish":
			kind = "fish"
			// fish makes ~/.config/fish on its first start, and reads
			// config.fish there when there is one
			if st, err := os.Stat(filepath.Dir(p)); err == nil && st.IsDir() {
				create = true
			}
		case "bash":
			if runtime.GOOS == "darwin" {
				if _, err := os.Lstat(p); os.IsNotExist(err) {
					h := filepath.Dir(p)
					for _, n := range []string{".bash_login", ".profile"} {
						if _, err := os.Lstat(filepath.Join(h, n)); err == nil {
							p = filepath.Join(h, n)
							break
						}
					}
				}
			}
		}
		out = append(out, ShellProfile{Kind: kind, Path: p, Create: create})
	}
	return out
}
