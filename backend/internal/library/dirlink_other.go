//go:build !windows

package library

import "os"

// dirLink makes link a link to the folder target.
func dirLink(target, link string) error { return os.Symlink(target, link) }
