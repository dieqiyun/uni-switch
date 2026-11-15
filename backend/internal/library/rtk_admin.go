package library

// RTK installed for every user of a Windows computer (winget --scope
// machine, under C:\Program Files\WinGet) is one only an administrator can
// upgrade, and uni-switch runs as the user (新v学员 on Discord: "RTK and
// other actions that need elevation all fail"). uni-switch isn't run as
// administrator for it: an elevated uni-switch would write the agents' files
// as the administrator, and couldn't start with Windows without asking each
// time. Instead the one winget command that needs it goes through Windows'
// administrator prompt (update.RunElevated), as an agent's npm update in
// Program Files does; a prompt declined or not to be had says so, and what
// to do, as agent.AdminError.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/update"
)

// wingetNeedsAdmin is winget saying the command needs an administrator
// (APPINSTALLER_CLI_ERROR_COMMAND_REQUIRES_ADMIN).
const wingetNeedsAdmin uint32 = 0x8A150019

var (
	// rtkElevates is whether an installer can be run through an
	// administrator prompt here (Windows); vars so tests can on any system
	rtkElevates = update.CanRunElevated()
	runElevated = update.RunElevated
	rtkWritable = update.Writable
	// runPlain is runInstaller: winget's HRESULTs are past what a test's
	// sh can exit with
	runPlain = runInstaller
)

// rtkAdminFolder is the folder of the rtk at bin when winget is to upgrade
// it and only an administrator can change that folder; "" otherwise.
func rtkAdminFolder(c []string, bin string) string {
	if !rtkElevates || len(c) == 0 || c[0] != "winget" {
		return ""
	}
	real, err := filepath.EvalSymlinks(bin)
	if err != nil {
		real = bin
	}
	if d := filepath.Dir(real); !rtkWritable(d) {
		return d
	}
	return ""
}

// runRTKInstaller runs installer c as runInstaller does, but winget's
// through Windows' administrator prompt when admin, the folder RTK is in,
// needs one, or when winget answers that it does.
func runRTKInstaller(c []string, timeout time.Duration, admin string, env ...string) (string, error) {
	elevates := rtkElevates && c[0] == "winget"
	if admin == "" || !elevates {
		out, err := runPlain(c, timeout, env...)
		var ie *installError
		if !elevates || !errors.As(err, &ie) || uint32(ie.code) != wingetNeedsAdmin {
			return out, err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	b, err := runElevated(ctx, home(), append(append(os.Environ(), "NONINTERACTIVE=1"), env...), c...)
	out := string(b)
	var ee *update.ExitError
	switch {
	case err == nil:
		return out, nil
	case errors.Is(err, update.ErrCanceled) || errors.Is(err, update.ErrNoAdmin):
		return out, &agent.AdminError{Agent: "RTK", Prefix: admin, Command: shown(c), Via: "winget", Canceled: errors.Is(err, update.ErrCanceled), Err: err}
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return out, installFailed(c, out, -1, fmt.Errorf("didn't finish in %s", timeout))
	case errors.As(err, &ee):
		return out, installFailed(c, out, int(ee.Code), err)
	}
	return out, installFailed(c, out, -1, fmt.Errorf("couldn't run: %w", err))
}
