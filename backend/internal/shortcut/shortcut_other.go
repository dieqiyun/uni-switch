//go:build !windows

package shortcut

func startMenuLink() (string, error) { return "", errNoStartMenu }

func writeShortcut(string, string) (bool, error) { return false, nil }

func registerAppPath(string) {}

func registeredAppPath() bool { return false }

func unregisterAppPath(string) error { return nil }
