//go:build !windows

package cli

import "syscall"

// agyExecOS runs agy in uni-switch's place: the terminal's signals and exit
// code are agy's own.
func agyExecOS(bin string, args, env []string) error {
	return syscall.Exec(bin, append([]string{"agy"}, args...), env)
}
