package claudetui

import (
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// openPTY opens a pseudo-terminal: its master, and the name of its slave.
func openPTY() (*os.File, string, error) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, "", err
	}
	fd := int(m.Fd())
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0); err != nil {
		m.Close()
		return nil, "", err
	}
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0); err != nil {
		m.Close()
		return nil, "", err
	}
	var name [128]byte
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&name[0]))); e != 0 {
		m.Close()
		return nil, "", e
	}
	n := 0
	for n < len(name) && name[n] != 0 {
		n++
	}
	return m, string(name[:n]), nil
}

// the requests that read and set a terminal's modes (the tests' fake
// Claude Code sets its own raw, as the real one does)
const ioctlGetTermios, ioctlSetTermios = unix.TIOCGETA, unix.TIOCSETA
