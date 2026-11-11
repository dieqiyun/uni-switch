package gui

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
	"github.com/dieqiyun/uni-switch/backend/internal/update"
)

// A folder on the way to uni-switch's own that this user can't write — most
// often a ~/.config a command run under sudo once made, so it belongs to
// root — fails every save with "permission denied" (YouWhat on Discord:
// adding a custom provider said "mkdir ~/.config/uni-switch: permission
// denied"). uni-switch's files stay where every reader looks for them; the
// window offers to make that folder the user's again, with one click and
// the system's password prompt. Only that folder changes: ~/.config gets
// its owner back and nothing in it does, since the other apps' folders
// there are theirs, and uni-switch's own folder, when it is the one taken, is
// taken back whole.

// configBlocked is the folder that keeps uni-switch from saving, as the window
// names it, or "".
func configBlocked() string {
	if b := appdir.Blocked(); b != "" {
		return tilde(b)
	}
	return ""
}

// permissionDenied says whether err is the system refusing a write.
func permissionDenied(err error) bool {
	if errors.Is(err, fs.ErrPermission) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "permission denied") || strings.Contains(msg, "operation not permitted")
}

// asAdmin runs a script as root after the system's password prompt; tests
// stand in for it.
var asAdmin = update.AsAdmin

// mine says whether this user owns a folder; tests stand in for it.
var mine = appdir.Mine

// errRepairCanceled is the password prompt dismissed.
var errRepairCanceled = update.ErrCanceled

// repairConfig makes the folder that keeps uni-switch from saving this user's
// again, then makes uni-switch's folders. prompt is whether the system's
// password prompt can be shown: not to uni-switch web's page on another
// device, where no one sits at this computer's screen to answer it.
func repairConfig(prompt bool) error {
	if b := appdir.Blocked(); b != "" {
		if mine(b) {
			// the user's own, only its mode is wrong: no password needed
			fi, err := os.Stat(b)
			if err != nil {
				return err
			}
			if err := os.Chmod(b, fi.Mode().Perm()|0o700); err != nil {
				return err
			}
		} else {
			recurse := ""
			if appdir.Own(b) {
				recurse = "-R " // uni-switch's own folder: everything in it is uni-switch's
			}
			q := update.ShellQuote(b)
			script := fmt.Sprintf("chown %s%d:%d %s && chmod u+rwx %s", recurse, os.Getuid(), os.Getgid(), q, q)
			if !prompt || !canElevate() {
				return fmt.Errorf("%s belongs to another user, and no administrator prompt can be shown here: run `sudo %s` in a terminal", b, script)
			}
			if err := asAdmin("uni-switch needs "+b+" to be yours again to save its settings.", script); err != nil {
				return err
			}
		}
	}
	for _, dir := range []string{appdir.Config(), appdir.Cache()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if b := appdir.Blocked(); b != "" {
		return fmt.Errorf("%s still can't be written", b)
	}
	return nil
}

// configRepairRoutes serves the window's Repair.
func configRepairRoutes(mux *http.ServeMux, prompt bool) {
	mux.HandleFunc("POST /api/config/repair", func(rw http.ResponseWriter, r *http.Request) {
		if err := repairConfig(prompt); err != nil {
			if errors.Is(err, errRepairCanceled) {
				writeJSON(rw, map[string]bool{"canceled": true})
				return
			}
			fail(rw, err)
			return
		}
		rw.WriteHeader(http.StatusNoContent)
	})
}
