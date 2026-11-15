//go:build !windows

package agent

func installedWindowsStorePackages() []string { return nil }

func registeredWindowsApp(string) (dir, version string) { return "", "" }
