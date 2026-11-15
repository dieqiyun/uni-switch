package edit

import (
	"os"

	"golang.org/x/sys/windows"
)

// hardLinked reports whether the file at path has other names too.
func hardLinked(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var fi windows.ByHandleFileInformation
	return windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &fi) == nil && fi.NumberOfLinks > 1
}

// keepGroup: Windows files have no Unix group to keep.
func keepGroup(*os.File, os.FileInfo) {}
