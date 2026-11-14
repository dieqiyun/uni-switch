// Package dirent decides whether an entry a reader lists is a folder to go
// into, links included.
package dirent

import (
	"io/fs"
	"os"
	"path/filepath"
)

// IsFolder is whether e, an entry listed in dir, is a folder to read into:
// a folder of its own, or a link that leads to one. os.ReadDir reports a
// symlink as ModeSymlink and, on Windows, a junction as ModeIrregular (Go
// 1.23 and later), and neither as IsDir, so a reader that goes into only
// IsDir() entries, or IsDir() and ModeSymlink ones, walks past a folder
// kept on another drive with a junction left where it was (#2268). Such an
// entry is followed with os.Stat. A link to a file, a broken link and
// anything else irregular are no folder.
//
// It follows only the entry it is asked about. A reader that recurses on
// it must keep its own record of the folders it has been in.
func IsFolder(dir string, e fs.DirEntry) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&(fs.ModeSymlink|fs.ModeIrregular) == 0 {
		return false
	}
	fi, err := os.Stat(filepath.Join(dir, e.Name()))
	return err == nil && fi.IsDir()
}
