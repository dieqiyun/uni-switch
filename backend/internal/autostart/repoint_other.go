//go:build !windows

package autostart

// repoint: only the Windows app moves itself (update.Settle)
func repoint(string, string) error { return nil }
