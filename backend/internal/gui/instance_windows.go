package gui

// Windows hands no gateway over (handover.go): a restart there waits for
// it to be idle, and the instance is Wails' to keep.

func releaseInstance()      {}
func retakeInstance()       {}
func handoffAlive(int) bool { return false }
