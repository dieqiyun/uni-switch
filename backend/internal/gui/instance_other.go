//go:build !darwin

package gui

// runningAlready is the Mac's; off it Wails' single instance hands a
// second launch over (singleInstance).
func runningAlready(showMain bool, link string) bool { return false }
