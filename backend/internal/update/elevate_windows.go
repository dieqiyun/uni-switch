package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// CanRunElevated reports whether RunElevated can ask for an
// administrator here: Windows' own prompt (UAC). uni-switch's self-update
// doesn't use it (CanElevate is false on Windows); an agent's npm, whose
// global folder is Node.js's under Program Files, does.
func CanRunElevated() bool { return true }

var shellExecuteEx = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// shellExecuteInfo is SHELLEXECUTEINFOW.
type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     windows.Handle
}

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
	seeMaskFlagNoUI       = 0x00000400
)

// RunElevated runs argv as administrator, after Windows asks the user
// (UAC), in dir with env, and returns what it printed. ErrCanceled is the
// prompt declined. A process started elevated gets the user's environment
// afresh, not this one's, so env's values that differ from uni-switch's own
// (the proxy uni-switch's requests take, a PATH put first) and PATH are set
// by the script it runs; its output comes back through a file, as an
// elevated process can't write into this one's pipes.
func RunElevated(ctx context.Context, dir string, env []string, argv ...string) ([]byte, error) {
	if len(argv) == 0 {
		return nil, errors.New("nothing to run")
	}
	tmp, err := os.MkdirTemp("", "uni-switch-elevated-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	log := filepath.Join(tmp, "out.log")
	script := filepath.Join(tmp, "run.cmd")
	body, err := elevatedScript(dir, env, os.Environ(), log, argv)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		return nil, err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"))
	if os.Getenv("SystemRoot") == "" {
		file, _ = windows.UTF16PtrFromString("cmd.exe")
	}
	params, _ := windows.UTF16PtrFromString(`/d /s /c ""` + script + `""`)
	info := shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess | seeMaskNoAsync | seeMaskFlagNoUI,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: params,
		nShow:        windows.SW_HIDE,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	if ok, _, e := shellExecuteEx.Call(uintptr(unsafe.Pointer(&info))); ok == 0 {
		if errors.Is(e, windows.ERROR_CANCELLED) {
			return nil, ErrCanceled
		}
		return nil, fmt.Errorf("%w: %v", ErrNoAdmin, e)
	}
	if info.hProcess == 0 {
		return nil, fmt.Errorf("%w: no process", ErrNoAdmin)
	}
	defer windows.CloseHandle(info.hProcess)
	for {
		ev, err := windows.WaitForSingleObject(info.hProcess, 200)
		if err != nil {
			return nil, err
		}
		if ev == windows.WAIT_OBJECT_0 {
			break
		}
		if ctx.Err() != nil {
			windows.TerminateProcess(info.hProcess, 1)
			out, _ := os.ReadFile(log)
			return out, ctx.Err()
		}
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.hProcess, &code); err != nil {
		return nil, err
	}
	// cmd.exe may still hold the file a moment after it ended
	var out []byte
	for i := 0; i < 10; i++ {
		if out, err = os.ReadFile(log); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if code != 0 {
		return out, &ExitError{Code: code}
	}
	return out, nil
}
