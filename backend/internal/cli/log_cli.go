package cli

// uni-switch log: where uni-switch's logs are, and the app's last lines. The app
// opened from the Finder, the Dock or the Start menu writes its log to a
// file (applog), which a user asked for it finds here or in Settings ›
// About › Log (rayem1w on X had been told of serve.log, which only the
// service writes).

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/applog"
	"github.com/dieqiyun/uni-switch/backend/internal/daemon"
	"github.com/dieqiyun/uni-switch/backend/internal/tui"
)

const logUsage = `usage: uni-switch log [--path] [-n N] [-f]
  where uni-switch's logs are on this computer: the app's own (the app, the menu bar and the
  panel write it, wherever they were opened from), the one before it, and the logs of
  uni-switch serve as a service (uni-switch daemon) or left running by uni-switch tui, when there are.
  --path  the app's log alone, its path only
  -n N    the app log's last N lines (100 with -f)
  -f      the app log's last lines, then each line as it is written, until Ctrl+C
  Send the app's log with a report. It holds what uni-switch did, not your keys, prompts or replies.`

// logCmd: uni-switch log [--path] [-n N] [-f]
func logCmd(args []string) error {
	pathOnly, follow, lines := false, false, -1
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--path":
			pathOnly = true
		case a == "-f" || a == "--follow":
			follow = true
		case a == "-n" || a == "--lines":
			if i+1 == len(args) {
				return errors.New("-n takes a number of lines (uni-switch log -n 200)")
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 0 {
				return fmt.Errorf("-n takes a number of lines, not %q (uni-switch log -n 200)", args[i])
			}
			lines = n
		default:
			return fmt.Errorf("unknown argument %s (uni-switch log [--path] [-n N] [-f])", a)
		}
	}
	path := applog.Path()
	switch {
	case pathOnly && (follow || lines >= 0):
		return errors.New("--path prints the path alone: use it without -n or -f")
	case pathOnly:
		fmt.Println(path)
		return nil
	case follow || lines >= 0:
		if lines < 0 {
			lines = 100
		}
		return tailLog(os.Stdout, path, lines, follow, nil)
	}
	fmt.Print(logsHere(path))
	return nil
}

// logsHere says where each of uni-switch's logs is: the app's, always, and
// the others that are there.
func logsHere(path string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", path)
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintf(&b, "  the app's log: nothing yet; the app writes it once it is opened\n")
	} else {
		fmt.Fprintf(&b, "  the app's log (Settings › About › Log); uni-switch log -f follows it\n")
	}
	if old := filepath.Join(filepath.Dir(path), applog.OldName); exists(old) {
		fmt.Fprintf(&b, "%s\n  the app's log before it, kept once it reached %d MiB\n", old, applog.Limit>>20)
	}
	if st, err := daemon.Get(); err == nil && st.Installed && st.Logs != "" {
		fmt.Fprintf(&b, "%s\n  uni-switch serve as a service (uni-switch daemon)\n", st.Logs)
	}
	if bg := tui.BackgroundLog(); exists(bg) {
		fmt.Fprintf(&b, "%s\n  uni-switch serve left running by uni-switch tui as it quit\n", bg)
	}
	return b.String()
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// tailLog writes the last n lines of the log at path to w, then, with
// follow, what is written to it after, until stop is closed (nil: until
// uni-switch is ended). A log rotated meanwhile is read again from its start.
func tailLog(w io.Writer, path string, n int, follow bool, stop <-chan struct{}) error {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && follow {
			fmt.Fprintf(os.Stderr, "uni-switch: %s isn't there yet: waiting for the app to write it\n", path)
		} else if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s isn't there yet: the app writes it once it is opened", path)
		} else {
			return err
		}
	}
	var at int64
	if f != nil {
		b, err := io.ReadAll(f)
		if err != nil {
			f.Close()
			return err
		}
		at = int64(len(b))
		w.Write(lastLines(b, n))
	}
	if !follow {
		if f != nil {
			f.Close()
		}
		return nil
	}
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	defer func() {
		if f != nil {
			f.Close()
		}
	}()
	for {
		select {
		case <-stop:
			return nil
		case <-tick.C:
		}
		// a new file at path (rotated, or written for the first time):
		// read it from its start
		if fi, err := os.Stat(path); err == nil {
			var mine os.FileInfo
			if f != nil {
				mine, _ = f.Stat()
			}
			if f == nil || mine == nil || !os.SameFile(fi, mine) || fi.Size() < at {
				if nf, err := os.Open(path); err == nil {
					if f != nil {
						// what the old file got after the last read first
						io.Copy(w, f)
						f.Close()
					}
					f, at = nf, 0
				}
			}
		}
		if f == nil {
			continue
		}
		n, _ := io.Copy(w, bufio.NewReader(f))
		at += n
	}
}

// lastLines is b's last n lines.
func lastLines(b []byte, n int) []byte {
	if n == 0 {
		return nil
	}
	i := len(b)
	if i > 0 && b[i-1] == '\n' {
		i--
	}
	for ; n > 0; n-- {
		i = bytes.LastIndexByte(b[:i], '\n')
		if i < 0 {
			return b
		}
	}
	return b[i+1:]
}
