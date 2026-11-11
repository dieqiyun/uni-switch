//go:build !unix

package sessions

// agyHeld: how agy locks a conversation it has open on Windows hasn't been
// seen, so there the minute since its files were written decides alone.
func agyHeld(string) bool { return false }
