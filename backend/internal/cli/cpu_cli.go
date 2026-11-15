package cli

// uni-switch cpu: a Go CPU profile of the running uni-switch, recorded for some
// seconds while the user goes on using it, for a report of uni-switch using
// too much CPU (@LeiCheng42 on X, while Codex runs). Activity Monitor and
// a macOS `sample` show Go's frames as ???, so the profile is asked of the
// process itself, through the gateway it serves on this computer
// (gateway.CPUPath, loopback only, as uni-switch heap's).

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

const cpuUsage = `usage: uni-switch cpu [<file>] [--seconds N]
  records a Go CPU profile of the uni-switch serving the gateway on this computer (the app,
  the menu bar, uni-switch serve or uni-switch web) for 30 seconds, or N from 5 to 120, for a
  report of uni-switch using too much CPU. While it records, use uni-switch as you do when it is
  busy: run Codex or whichever agent it was. The profile is written to <file>, else to
  uni-switch-cpu-<date>-<time>.pb.gz in this folder, and never over a file that is there.
  uni-switch then prints the file, the CPU time the process used while recording and its
  goroutines: send both. The profile holds where the time went (function names, source
  lines) and the path of the uni-switch program, not keys or messages; go tool pprof <file>
  reads it. It asks the gateway at the port in Settings' Gateway address (or UNI_SWITCH_ADDR) over loopback.`

// cpuArgs reads uni-switch cpu's arguments: the file, "" for the default, and
// the seconds to record.
func cpuArgs(args []string) (file string, secs int, err error) {
	const form = "(uni-switch cpu [<file>] [--seconds N])"
	secs = gateway.CPUSeconds
	seen := false
	setSecs := func(v string) error {
		n, err := strconv.Atoi(v)
		if err != nil || n < gateway.CPUMinSeconds || n > gateway.CPUMaxSeconds {
			return fmt.Errorf("--seconds is a whole number from %d to %d, not %q: nothing was recorded %s", gateway.CPUMinSeconds, gateway.CPUMaxSeconds, v, form)
		}
		if seen {
			return fmt.Errorf("--seconds is given twice: nothing was recorded %s", form)
		}
		seen, secs = true, n
		return nil
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--seconds":
			if i+1 >= len(args) {
				return "", 0, fmt.Errorf("--seconds needs a number: nothing was recorded %s", form)
			}
			i++
			if err := setSecs(args[i]); err != nil {
				return "", 0, err
			}
		case strings.HasPrefix(a, "--seconds="):
			if err := setSecs(strings.TrimPrefix(a, "--seconds=")); err != nil {
				return "", 0, err
			}
		case strings.HasPrefix(a, "-") && a != "-":
			return "", 0, fmt.Errorf("unknown flag %s: nothing was recorded %s", a, form)
		case a == "-" || strings.TrimSpace(a) == "":
			return "", 0, fmt.Errorf("%q is not a file name: nothing was recorded %s", a, form)
		case file != "":
			return "", 0, fmt.Errorf("uni-switch cpu takes one file: nothing was recorded %s", form)
		default:
			file = a
		}
	}
	return file, secs, nil
}

// cpuCmd: uni-switch cpu [<file>] [--seconds N]
func cpuCmd(args []string) error {
	file, secs, err := cpuArgs(args)
	if err != nil {
		return err
	}
	if file == "" {
		file = "uni-switch-cpu-" + time.Now().Format("20060102-150405") + ".pb.gz"
	}
	// before the seconds of recording, not after them: the file is
	// opened without writing over one again once the profile is here
	if _, err := os.Lstat(file); err == nil {
		return fmt.Errorf("%s is there already: name another file (uni-switch cpu <file>)", file)
	}
	base := gateway.URL()
	if err := pingUniSwitch(base); err != nil {
		return err
	}
	fmt.Printf("%s uni-switch's CPU for %ds, until %s. Use uni-switch as you do when it is busy (run Codex) until then.\n",
		green.Render("Recording"), secs, time.Now().Add(time.Duration(secs)*time.Second).Format("15:04:05"))
	prof, stats, err := fetchCPU(base, secs)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s is there already: name another file (uni-switch cpu <file>)", file)
		}
		return err
	}
	if _, err := f.Write(prof); err != nil {
		f.Close()
		os.Remove(file)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(file)
		return err
	}
	if abs, err := filepath.Abs(file); err == nil {
		file = abs
	}
	fmt.Printf("%s %s (%s)\n", green.Render("saved"), file, bytesSize(uint64(len(prof))))
	fmt.Print(cpuSummary(stats))
	fmt.Println(muted.Render("Send the file and these lines with the report."))
	return nil
}

// pinguni-switch checks that a uni-switch answers at base before the seconds of
// recording are waited out.
func pingUniSwitch(base string) error {
	c := &http.Client{Timeout: 5 * time.Second}
	res, err := c.Get(base + "/")
	if err != nil {
		return fmt.Errorf("no uni-switch answers at %s, so there is nothing to profile: is uni-switch running? (%v)", base, err)
	}
	defer res.Body.Close()
	io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	return nil
}

// fetchCPU asks the gateway at base for a profile of secs seconds: its
// bytes, checked to be gzip, and the process's numbers beside it.
func fetchCPU(base string, secs int) ([]byte, gateway.CPUStats, error) {
	var st gateway.CPUStats
	// the seconds recorded, and room for a busy machine to send it
	c := &http.Client{Timeout: time.Duration(secs)*time.Second + 2*time.Minute}
	res, err := c.Get(base + gateway.CPUPath + "?seconds=" + url.QueryEscape(strconv.Itoa(secs)))
	if err != nil {
		return nil, st, fmt.Errorf("no uni-switch answers at %s, so there is nothing to profile: is uni-switch running? (%v)", base, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, st, fmt.Errorf("the profile from %s was cut off: %w", base, err)
	}
	if res.StatusCode == http.StatusNotFound {
		return nil, st, fmt.Errorf("the uni-switch at %s is older than this one and has no CPU profile: update it, then run uni-switch cpu again", base)
	}
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		msg := strings.TrimSpace(string(body))
		if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
			msg = e.Error.Message
		}
		return nil, st, fmt.Errorf("the uni-switch at %s answered %s: %s", base, res.Status, msg)
	}
	if !bytes.HasPrefix(body, []byte{0x1f, 0x8b}) {
		return nil, st, fmt.Errorf("what %s answered is not a CPU profile (%d bytes): is it uni-switch?", base, len(body))
	}
	if err := json.Unmarshal([]byte(res.Header.Get(gateway.CPUStatsHeader)), &st); err != nil {
		return nil, st, fmt.Errorf("the uni-switch at %s sent no CPU numbers with its profile: %v", base, err)
	}
	return body, st, nil
}

// cpuSummary is the lines uni-switch cpu prints of st.
func cpuSummary(st gateway.CPUStats) string {
	var b strings.Builder
	who := st.Program
	if st.Window {
		who += ", with a window"
	}
	fmt.Fprintf(&b, "  process     %d (%s) uni-switch %s, up %s\n", st.PID, who, st.Version, st.Uptime)
	if st.CPUKnown && st.Seconds > 0 {
		fmt.Fprintf(&b, "  CPU         %.2fs in the %.1fs recorded (%.0f%% of one core)\n", st.CPU, st.Seconds, 100*st.CPU/st.Seconds)
		fmt.Fprintf(&b, "  CPU total   %.1fs since it started\n", st.CPUTotal)
	} else {
		fmt.Fprintf(&b, "  CPU         the system didn't say (%.1fs recorded)\n", st.Seconds)
	}
	fmt.Fprintf(&b, "  cores       %d (GOMAXPROCS %d)\n", st.NumCPU, st.GOMAXPROCS)
	fmt.Fprintf(&b, "  goroutines  %d\n", st.Goroutines)
	return b.String()
}
