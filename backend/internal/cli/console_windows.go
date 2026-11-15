package cli

import (
	"os"
	"syscall"

	"github.com/charmbracelet/lipgloss"
	"github.com/dieqiyun/uni-switch/backend/internal/update"
)

// The desktop build is linked as a GUI program so double-clicking it opens
// no console window; such a program starts with no standard streams. When it
// is run from a terminal with a command, borrow that terminal's console so
// `uni-switch ls` and friends still print. Output that is redirected already has
// somewhere to go and is left alone.
//
// uni-switch.com, the console program a terminal runs for `uni-switch` (#3737,
// update.EnsureCompanion), hands over its own standard handles, redirected
// or the terminal's, and says so in update.ComEnv: they are kept, and its
// console is borrowed too, so Ctrl+C, a hidden prompt and the programs
// uni-switch starts have it. The variable is taken out of the environment, so
// what uni-switch starts doesn't read it as its own.
func init() {
	fromCom := os.Getenv(update.ComEnv) != ""
	if fromCom {
		os.Unsetenv(update.ComEnv)
	}
	if len(os.Args) < 2 {
		return
	}
	attach := syscall.NewLazyDLL("kernel32.dll").NewProc("AttachConsole")
	const attachParentProcess = ^uintptr(0) // (DWORD)-1
	if fromCom {
		if r, _, _ := attach.Call(attachParentProcess); r != 0 {
			// Windows doesn't give a GUI program the console handles
			// it was started with, only the redirected ones: those
			// left out are the console's own, set as it is attached
			for _, std := range []struct {
				f    **os.File
				n    int
				was  syscall.Handle
				name string
			}{
				{&os.Stdin, syscall.STD_INPUT_HANDLE, syscall.Stdin, "/dev/stdin"},
				{&os.Stdout, syscall.STD_OUTPUT_HANDLE, syscall.Stdout, "/dev/stdout"},
				{&os.Stderr, syscall.STD_ERROR_HANDLE, syscall.Stderr, "/dev/stderr"},
			} {
				if t, err := syscall.GetFileType(std.was); err == nil && t != 0 {
					continue
				}
				if h, err := syscall.GetStdHandle(std.n); err == nil && h != 0 && h != syscall.InvalidHandle {
					*std.f = os.NewFile(uintptr(h), std.name)
				}
			}
			pointStyles(os.Stdout)
		}
		companion()
		return
	}
	if t, err := syscall.GetFileType(syscall.Stdout); err == nil && t != 0 {
		return // FILE_TYPE_UNKNOWN is 0: nothing attached
	}
	if r, _, _ := attach.Call(attachParentProcess); r == 0 {
		return
	}
	if out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
		os.Stdout, os.Stderr = out, out
		pointStyles(out)
	}
	if in, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		os.Stdin = in
	}
	companion()
}

// companion has a command run in a terminal bring the uni-switch.com beside
// this app up, or write it there when the folder is on PATH: run as
// uni-switch.exe, the first command after an update to this version, the next
// `uni-switch` is waited for (update.EnsureCompanion).
func companion() {
	if exe, err := update.Executable(); err == nil {
		update.EnsureCompanion(exe)
	}
}

// pointStyles points lipgloss at out: its default renderer was made over
// the os.Stdout before this, a handle that was no terminal then, so every
// style would print without colour. The styles made at package init hold
// that renderer, so it is pointed at the console in place. Anything else
// that reads or writes the standard streams takes os.Stdin, os.Stdout or
// os.Stderr when it runs, never in a package variable.
func pointStyles(out *os.File) {
	lipgloss.DefaultRenderer().SetOutput(lipgloss.NewRenderer(out).Output())
}
