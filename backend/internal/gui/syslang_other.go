//go:build !darwin && !windows

package gui

import "os"

// systemLang is the system's language, as the environment names it.
func systemLang() string { return envLang(os.Getenv) }
