package library

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// rtk's hooks run it by name — Pi's extension asks rtk --version and turns
// itself off ("RTK disabled: rtk binary not found in PATH") when that
// fails, Claude Code's and Codex's hooks are rtk hook …, and the commands
// they rewrite to are rtk git status and so on — so an agent needs rtk on
// its own PATH. uni-switch finds rtk in more places than that: the folders
// installers use, which the app adds to its PATH whether the user's shell
// has them or not (~/.local/bin, where rtk's own script puts it and only
// warns when it isn't on PATH), and winget's, whose link isn't always made.
// So uni-switch says when rtk isn't on the PATH a terminal opened now has, and
// puts it there when asked: a link in a folder on it, or on Windows its
// folder added to the user's PATH, as winget does when it can't link.
// Shell profiles are left alone.

// userPath is the PATH an agent started now gets (proc.LoginPath); nil
// when it can't be told. A test sets it.
var userPath = proc.LoginPath

func rtkName() string {
	if runtime.GOOS == "windows" {
		return "rtk.exe"
	}
	return "rtk"
}

// rtkReached says whether an agent started now finds rtk by name; true when
// the PATH it gets can't be told.
func rtkReached() bool {
	dirs := userPath()
	if dirs == nil {
		return true
	}
	for _, d := range dirs {
		if d != "" && runnable(filepath.Join(d, rtkName())) {
			return true
		}
	}
	return false
}

func runnable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && (runtime.GOOS == "windows" || st.Mode()&0o111 != 0)
}

// writable says whether a file can be made in dir.
func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".uni-switch-")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

// PathRTK puts the rtk uni-switch found on the PATH agents get, when the user
// asks for it (see rtkOnPath).
func PathRTK() (*RTKView, error) {
	rtkMu.Lock()
	defer rtkMu.Unlock()
	bin := rtkPath()
	if bin == "" {
		return nil, fmt.Errorf("rtk isn't installed — install it first (%s)", RTKURL)
	}
	if rtkReached() {
		return ReadRTK(), nil
	}
	dir, link := rtkPathDir(bin)
	if dir == "" {
		return nil, fmt.Errorf("uni-switch found no folder on your PATH it can put rtk in — add %s to PATH in your shell profile", filepath.Dir(bin))
	}
	if err := rtkOnPath(bin, dir, link); err != nil {
		return nil, err
	}
	v := ReadRTK()
	if v.OffPath {
		return nil, fmt.Errorf("rtk still isn't on your PATH after putting it in %s", dir)
	}
	for _, a := range v.Agents {
		if a.On {
			v.Restart = append(v.Restart, a.ID)
		}
	}
	return v, nil
}
