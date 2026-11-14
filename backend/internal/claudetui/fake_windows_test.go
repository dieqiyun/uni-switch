package claudetui

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// fakeRaw makes the fake Claude Code's console raw, as the real one's is:
// keys read as they come, VT sequences in (a paste's brackets) and out.
func fakeRaw() {
	in, _ := windows.GetStdHandle(windows.STD_INPUT_HANDLE)
	var m uint32
	if windows.GetConsoleMode(in, &m) == nil {
		m &^= windows.ENABLE_LINE_INPUT | windows.ENABLE_ECHO_INPUT | windows.ENABLE_PROCESSED_INPUT
		_ = windows.SetConsoleMode(in, m|windows.ENABLE_VIRTUAL_TERMINAL_INPUT)
	}
	out, _ := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if windows.GetConsoleMode(out, &m) == nil {
		_ = windows.SetConsoleMode(out, m|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
	}
}

// fakeScript is a batch file in dir that runs the test binary exe as the
// fake Claude Code, and its text: a .cmd, as npm installs Claude Code.
func fakeScript(dir, exe string) (string, string) {
	return filepath.Join(dir, "claude.cmd"), "@set UNI_SWITCH_TUI_TEST=claude\r\n@\"" + exe + "\" %*\r\n"
}
