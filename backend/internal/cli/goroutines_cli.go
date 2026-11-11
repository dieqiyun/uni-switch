package cli

// uni-switch goroutines: every goroutine of the running uni-switch, with its stack
// and how long it has waited, for a report of uni-switch stuck at something
// while it goes on running (#3362). Killing the app to see would end what
// was stuck, so the dump is asked of the process itself, through the
// gateway it serves on this computer (gateway.GoroutinesPath, loopback
// only, as uni-switch heap's).

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

const goroutinesUsage = `usage: uni-switch goroutines [<file>]
  every goroutine of the uni-switch serving the gateway on this computer (the app, the menu
  bar, uni-switch serve or uni-switch web), with its stack and how long it has waited, for a
  report of uni-switch stuck at something while it runs. uni-switch keeps running; nothing is
  stopped. It is written to <file>, else to uni-switch-goroutines-<date>-<time>.txt in this
  folder, and never over a file that is there. uni-switch then prints the file and which
  process it is of: send both. The dump holds function names, source lines and the path of
  the uni-switch program, with arguments as numbers only, not keys or messages.
  It asks the gateway at the port in Settings' Gateway address (or UNI_SWITCH_ADDR) over loopback.`

// goroutinesWait bounds the ask: a dump of thousands of goroutines is
// written in well under a second, so a uni-switch that doesn't answer in this
// is stuck where its gateway answers too.
const goroutinesWait = 30 * time.Second

// goroutinesCmd: uni-switch goroutines [<file>]
func goroutinesCmd(args []string) error {
	file := ""
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "-") && a != "-":
			return fmt.Errorf("unknown flag %s: nothing was written (uni-switch goroutines [<file>])", a)
		case a == "-" || strings.TrimSpace(a) == "":
			return fmt.Errorf("%q is not a file name: nothing was written (uni-switch goroutines [<file>])", a)
		case file != "":
			return errors.New("uni-switch goroutines takes one file: nothing was written (uni-switch goroutines [<file>])")
		default:
			file = a
		}
	}
	if file == "" {
		file = "uni-switch-goroutines-" + time.Now().Format("20060102-150405") + ".txt"
	}
	dump, stats, err := fetchGoroutines(gateway.URL())
	if err != nil {
		return err
	}
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s is there already: name another file (uni-switch goroutines <file>)", file)
		}
		return err
	}
	if _, err := f.Write(dump); err != nil {
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
	fmt.Printf("%s %s (%s)\n", green.Render("saved"), file, bytesSize(uint64(len(dump))))
	fmt.Print(goroutinesSummary(stats))
	fmt.Println(muted.Render("Send the file and these lines with the report."))
	return nil
}

// fetchGoroutines asks the gateway at base for the dump: its text, checked
// to be one, and which process it is of.
func fetchGoroutines(base string) ([]byte, gateway.GoroutineStats, error) {
	var st gateway.GoroutineStats
	c := &http.Client{Timeout: goroutinesWait}
	res, err := c.Get(base + gateway.GoroutinesPath)
	if err != nil {
		return nil, st, fmt.Errorf("no uni-switch answers at %s, so there is nothing to dump: is uni-switch running? (%v)", base, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, st, fmt.Errorf("the dump from %s was cut off: %w", base, err)
	}
	if res.StatusCode == http.StatusNotFound {
		return nil, st, fmt.Errorf("the uni-switch at %s is older than this one and has no goroutine dump: update it, then run uni-switch goroutines again", base)
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
	if !bytes.HasPrefix(body, []byte("goroutine ")) {
		return nil, st, fmt.Errorf("what %s answered is not a goroutine dump (%d bytes): is it uni-switch?", base, len(body))
	}
	if err := json.Unmarshal([]byte(res.Header.Get(gateway.GoroutineStatsHeader)), &st); err != nil {
		return nil, st, fmt.Errorf("the uni-switch at %s sent no process numbers with its dump: %v", base, err)
	}
	return body, st, nil
}

// goroutinesSummary is the lines uni-switch goroutines prints of st.
func goroutinesSummary(st gateway.GoroutineStats) string {
	var b strings.Builder
	who := st.Program
	if st.Window {
		who += ", with a window"
	}
	fmt.Fprintf(&b, "  process     %d (%s) uni-switch %s, up %s\n", st.PID, who, st.Version, st.Uptime)
	fmt.Fprintf(&b, "  goroutines  %d\n", st.Goroutines)
	return b.String()
}
