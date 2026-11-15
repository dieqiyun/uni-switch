//go:build !windows

package update

import (
	"path/filepath"
	"strings"
)

func hiddenForTest(p string) bool { return strings.HasPrefix(filepath.Base(p), ".") }
