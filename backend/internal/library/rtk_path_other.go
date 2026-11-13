//go:build !windows

package library

import (
	"os"
	"path/filepath"
	"slices"
)

// rtkPathDir is the folder on the user's PATH a link to rtk goes in: the
// first of those a user's tools usually are in that is on it, can be
// written to and has no rtk in it; "" when none is. link is always true:
// a link keeps up with rtk upgraded where it is.
func rtkPathDir(string) (dir string, link bool) {
	on := userPath()
	h := home()
	for _, d := range []string{
		filepath.Join(h, ".local", "bin"), filepath.Join(h, "bin"),
		"/opt/homebrew/bin", "/usr/local/bin", "/home/linuxbrew/.linuxbrew/bin",
		filepath.Join(h, ".cargo", "bin"),
	} {
		if !slices.Contains(on, d) {
			continue
		}
		if _, err := os.Lstat(filepath.Join(d, "rtk")); err == nil {
			continue // something called rtk that doesn't run
		}
		if st, err := os.Stat(d); err == nil && st.IsDir() && writable(d) {
			return d, true
		}
	}
	return "", true
}

// rtkOnPath links rtk into dir.
func rtkOnPath(bin, dir string, _ bool) error {
	return os.Symlink(bin, filepath.Join(dir, "rtk"))
}
