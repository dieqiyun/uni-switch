//go:build !windows

package gui

import "errors"

func startWindowsInstaller(string, string) error {
	return errors.New("Windows installer is unavailable on this platform")
}
