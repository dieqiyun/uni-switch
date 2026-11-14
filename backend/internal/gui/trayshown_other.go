//go:build (!darwin || !cgo) && !nogui

package gui

// Only the Mac keeps a status item's visibility (trayshown_darwin.go).
func forgetTrayHidden() int             { return 0 }
func forgetTrayHiddenLater()            {}
func bringTrayBack() (stillHidden bool) { return false }
func keepTrayPlace() int                { return 0 }
