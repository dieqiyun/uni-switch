//go:build !windows

package proc

// endTree is end: a program that ends on SIGTERM ends its own children.
func endTree(pid int) error { return end(pid) }
