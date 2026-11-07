package edit

import (
	"os"
	"syscall"
)

func reparse(fi os.FileInfo) bool {
	s, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	return ok && s.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
