//go:build !windows

package appdir

// osRegEnv is nil: only Windows has a registry its variables come from.
var osRegEnv func(name string) (string, bool)
