//go:build !darwin || nogui

package gui

// canDragFile: only the Mac's window hands a file to the system's drag;
// elsewhere the file manager shows it, to drag from there.
const canDragFile = false

func dragFile(string) bool { return false }
