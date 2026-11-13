package gui

import (
	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
	"os"
	"path/filepath"
)

// downloads is where an export is saved: the Downloads folder the system
// keeps (appdir.Downloads: Windows' known folder, the XDG one on Linux,
// which 1Shin on Discord had moved off home\Downloads), else
// home\Downloads, else home when there is none.
func downloads() string {
	home, _ := os.UserHomeDir()
	for _, d := range []string{appdir.Downloads(), filepath.Join(home, "Downloads")} {
		if d != "" && isDir(d) {
			return d
		}
	}
	return home
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
