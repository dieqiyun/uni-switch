//go:build !windows

package appdir

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// Blocked is the folder that keeps uni-switch from writing its own files, or ""
// when it can: uni-switch's config or cache folder that this user can't write,
// or, while one of them isn't there yet, the folder it would be made in. A
// ~/.config that a command run under sudo made belongs to root, so uni-switch
// couldn't make ~/.config/uni-switch and every save failed with "permission
// denied" (YouWhat on Discord). Only a folder inside the home folder is
// named: one outside it (an XDG_CONFIG_HOME on a shared disk) isn't the
// user's to take back.
func Blocked() string {
	h, err := Home()
	if err != nil {
		return ""
	}
	for _, dir := range []string{Config(), Cache()} {
		if b := blockedAt(dir); b != "" && inside(b, h) {
			return b
		}
	}
	return ""
}

// blockedAt is the folder on the way to dir this user can't write, "" when
// dir is writable or can be made.
func blockedAt(dir string) string {
	for p := filepath.Clean(dir); ; {
		fi, err := os.Stat(p)
		switch {
		case err == nil:
			if !fi.IsDir() || writable(p) {
				return "" // a file in the way isn't a matter of owners
			}
			return p
		case !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fs.ErrPermission):
			return ""
		}
		up := filepath.Dir(p)
		if up == p {
			return ""
		}
		p = up
	}
}

// writable says whether this user can make and change files in dir.
func writable(dir string) bool {
	return syscall.Access(dir, 0x2|0x1) == nil // W_OK|X_OK
}

// inside says whether p is strictly inside dir.
func inside(p, dir string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(p))
	return err == nil && rel != "." && rel != ".." && !filepath.IsAbs(rel) && !startsUp(rel)
}

func startsUp(rel string) bool {
	return len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator)
}

// Own says whether p is uni-switch's own folder or inside it: its config or
// cache folder. A repair may take back everything in it; anything above
// it is changed only by itself.
func Own(p string) bool {
	for _, dir := range []string{Config(), Cache()} {
		if c := filepath.Clean(p); c == filepath.Clean(dir) || inside(c, dir) {
			return true
		}
	}
	return false
}

// Mine says whether this user owns p, so its mode can be put right without
// an administrator.
func Mine(p string) bool {
	fi, err := os.Stat(p)
	if err != nil {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}
