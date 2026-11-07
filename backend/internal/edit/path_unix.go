//go:build !windows

package edit

import "os"

func reparse(os.FileInfo) bool { return false }
