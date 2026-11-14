//go:build !darwin || !cgo

package gui

// dockFlap: only macOS has a Dock tile to flap.
func dockFlap() {}
