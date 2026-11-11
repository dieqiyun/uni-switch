//go:build darwin || linux

package claudetui

import "golang.org/x/sys/unix"

// fakeRaw makes the fake Claude Code's terminal raw, as the real one's is: a
// paste is taken whole, and Enter outside one sends it.
func fakeRaw() {
	if tio, err := unix.IoctlGetTermios(0, ioctlGetTermios); err == nil {
		tio.Lflag &^= unix.ECHO | unix.ICANON
		tio.Iflag &^= unix.ICRNL
		_ = unix.IoctlSetTermios(0, ioctlSetTermios, tio)
	}
}

// fakeScript is a script in dir that runs the test binary exe as the fake
// Claude Code, and its path.
func fakeScript(dir, exe string) (string, string) {
	return dir + "/claude", "#!/bin/sh\nUNI_SWITCH_TUI_TEST=claude exec '" + exe + "' \"$@\"\n"
}
