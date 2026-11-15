package agent

// Claude Science is Anthropic's research app (0.1.62, "operon" inside): a
// daemon, claude-science, that serves its UI on localhost, and a window
// that shows it (a Swift launcher on the Mac, Electron on Windows, a
// browser on Linux) and attaches to the daemon running, else starts one.
// The daemon asks Anthropic's Messages API at its environment's
// ANTHROPIC_BASE_URL (ky() in its bundle: https, or http on a loopback
// address) and has no setting for it, so uni-switch connects it by starting
// the daemon itself with that variable set to the gateway — `stop`, then
// `serve --detached --no-browser`, on the port it was on — and nothing
// else: no variable set for the user, no file of its written. Disconnect
// restarts it without the variable. The app still needs its own Claude
// sign-in, which it sends as its bearer and the gateway never passes on
// (see gateway/science.go); with none it asks nothing at all.
//
// The daemon lives till it is stopped or the computer restarts, so while
// it is connected uni-switch starts it again when it isn't running
// (KeepClaudeScience), and says so when another one runs in its place —
// the window, opened while none ran, starts its own without the variable
// (Check).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// ScienceID is the agent's id.
const ScienceID = gateway.ScienceID

const scienceName = "Claude Science"

// scienceVar is the variable its daemon reads the API's address from.
const scienceVar = "ANTHROPIC_BASE_URL"

// scienceBin is the daemon's program, "" when it isn't installed. A var so
// tests point it elsewhere.
var scienceBin = func() string {
	scienceSeen.Lock()
	defer scienceSeen.Unlock()
	if time.Since(scienceSeen.at) > 30*time.Second {
		home, _ := os.UserHomeDir()
		scienceSeen.bin, scienceSeen.at = findScience(home), time.Now()
	}
	return scienceSeen.bin
}

var scienceSeen struct {
	sync.Mutex
	bin string
	at  time.Time
}

// findScience is where claude-science is installed for home: in the Mac
// app's bundle, Windows' per-user Programs folder, or the ~/.local/bin the
// installers link it into (the only place on Linux), else on PATH.
func findScience(home string) string {
	var at []string
	switch runtime.GOOS {
	case "darwin":
		for _, d := range macAppFolders(home) {
			at = append(at, filepath.Join(d, scienceName+".app", "Contents", "Resources", "bin", "claude-science"))
		}
	case "windows":
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			at = append(at, filepath.Join(d, "Programs", "ClaudeScience", "claude-science.exe"))
		}
		at = append(at, filepath.Join(home, "AppData", "Local", "Programs", "ClaudeScience", "claude-science.exe"))
	}
	exe := "claude-science"
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	at = append(at, filepath.Join(home, ".local", "bin", exe))
	for _, p := range at {
		if isFile(p) {
			return p
		}
	}
	if p, err := proc.LookPath("claude-science"); err == nil {
		return p
	}
	return ""
}

// scienceRun runs the daemon's program bin with args, its environment env
// (nil: uni-switch's own), and is what it printed. A var so tests never start
// the real one. serve --detached returns once its daemon is up; the
// daemon keeps none of the pipes, which go to no file at all.
var scienceRun = func(env []string, bin string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := proc.CommandContext(ctx, bin, args...)
	cmd.Env = env
	if len(args) > 0 && args[0] == "serve" {
		// a detached daemon holding a pipe would hold Output till it quits
		return nil, cmd.Run()
	}
	return cmd.Output()
}

// scienceStatus is what `claude-science status` says of its daemon.
type scienceStatus struct {
	Running bool   `json:"running"`
	PID     int    `json:"pid"`
	Port    int    `json:"port"`
	Version string `json:"version"`
}

// scienceDir is the daemon's data folder, ~/.claude-science. A var so
// tests point it elsewhere.
var scienceDir = func() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude-science")
}

// scienceLock is the daemon running as its lock file says (operon.lock:
// its pid, port and start), false when there is none. It is read where
// status would take seconds — the program is a 130 MB Bun build — as the
// Agents page is drawn; a lock a crash left is taken for the daemon,
// which only Check reads it for.
func scienceLock() (scienceStatus, bool) {
	var st scienceStatus
	b, err := os.ReadFile(filepath.Join(scienceDir(), "operon.lock"))
	if err != nil || json.Unmarshal(b, &st) != nil || st.PID == 0 {
		return st, false
	}
	st.Running = true
	return st, true
}

func scienceState(bin string) (scienceStatus, error) {
	var st scienceStatus
	out, err := scienceRun(nil, bin, "status")
	if err != nil {
		return st, fmt.Errorf("claude-science status: %w", err)
	}
	if err := json.Unmarshal(out, &st); err != nil {
		return st, fmt.Errorf("claude-science status said %q: %w", strings.TrimSpace(string(out)), err)
	}
	return st, nil
}

// scienceMark is uni-switch's note of the daemon it started on the gateway,
// which the row reads as connected.
type scienceMark struct {
	URL  string `json:"url"`
	PID  int    `json:"pid"`
	Port int    `json:"port,omitempty"` // the UI's, which a start again keeps
}

func scienceMarkPath() string { return filepath.Join(appdir.Config(), "claude-science.json") }

func readScienceMark() (scienceMark, bool) {
	var m scienceMark
	b, err := os.ReadFile(scienceMarkPath())
	if err != nil {
		return m, false
	}
	// one that doesn't parse is still uni-switch's note that it is connected
	_ = json.Unmarshal(b, &m)
	return m, true
}

func scienceWired() bool { _, ok := readScienceMark(); return ok }

// scienceEnv is uni-switch's environment with the variable set to gw, or
// without it for "".
func scienceEnv(gw string) []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); !strings.EqualFold(k, scienceVar) {
			env = append(env, kv)
		}
	}
	if gw != "" {
		env = append(env, scienceVar+"="+gw)
	}
	return env
}

// scienceMu holds one start or stop of the daemon at a time.
var scienceMu sync.Mutex

// scienceRestart stops the daemon when it runs and starts it on gw ("":
// without the variable), on the port it was on; only stops it for !start.
// It is the daemon now running.
func scienceRestart(bin, gw string, start bool) (scienceStatus, error) {
	was, err := scienceState(bin)
	if err != nil {
		return was, err
	}
	if was.Running {
		if out, err := scienceRun(nil, bin, "stop"); err != nil {
			return was, fmt.Errorf("claude-science stop: %w %s", err, strings.TrimSpace(string(out)))
		}
	}
	if !start {
		return scienceStatus{}, nil
	}
	return scienceServe(bin, gw, was.Port)
}

// scienceServe starts the daemon on gw ("": without the variable), on
// port when it isn't 0, and waits till status says it runs.
func scienceServe(bin, gw string, port int) (scienceStatus, error) {
	args := []string{"serve", "--detached", "--no-browser"}
	if port > 0 {
		args = append(args, "--port", strconv.Itoa(port))
	}
	if _, err := scienceRun(scienceEnv(gw), bin, args...); err != nil {
		return scienceStatus{}, fmt.Errorf("claude-science serve: %w", err)
	}
	var st scienceStatus
	for deadline := time.Now().Add(scienceWait); ; {
		var err error
		if st, err = scienceState(bin); err == nil && st.Running {
			return st, nil
		}
		if time.Now().After(deadline) {
			return st, errors.New("claude-science serve: its daemon isn't running (see claude-science logs)")
		}
		time.Sleep(scienceWait / 60)
	}
}

// scienceWait is how long a daemon started may take to say it runs.
var scienceWait = 30 * time.Second

// scienceOn restarts the daemon on the gateway, and notes it; off
// restarts it without the variable, when it ran, and drops the note.
func scienceOn(on bool) error {
	scienceMu.Lock()
	defer scienceMu.Unlock()
	bin := scienceBin()
	if !on {
		if _, wired := readScienceMark(); wired && bin != "" {
			st, err := scienceState(bin)
			if err != nil {
				return err
			}
			if st.Running {
				if _, err := scienceRestart(bin, "", true); err != nil {
					return err
				}
			}
		}
		if err := os.Remove(scienceMarkPath()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if bin == "" {
		return errors.New(scienceName + " isn't installed (no claude-science found)")
	}
	gw := gateway.URL()
	// the daemon uni-switch started, on this gateway, is left running: Connect
	// sets the field it connected by again (Pick)
	if m, wired := readScienceMark(); wired && m.URL == gw {
		if st, err := scienceState(bin); err == nil && st.Running && st.PID == m.PID {
			return nil
		}
	}
	st, err := scienceRestart(bin, gw, true)
	if err != nil {
		return err
	}
	return writeScienceMark(scienceMark{URL: gw, PID: st.PID, Port: st.Port})
}

func writeScienceMark(m scienceMark) error {
	b, _ := json.Marshal(m)
	if err := os.MkdirAll(filepath.Dir(scienceMarkPath()), 0o700); err != nil {
		return err
	}
	return os.WriteFile(scienceMarkPath(), b, 0o600)
}

// scienceCheck is what of the wiring is gone, by the daemon's lock file: a
// daemon running that uni-switch
// didn't start (the window, opened while none ran, starts its own), or one
// started for the gateway's old address. One not running is no drift:
// KeepClaudeScience starts it.
func scienceCheck() string {
	m, wired := readScienceMark()
	if !wired {
		return ""
	}
	st, running := scienceLock()
	if !running {
		return ""
	}
	if st.PID != m.PID {
		return fmt.Sprintf("%s's daemon (pid %d) was started without uni-switch's gateway", scienceName, st.PID)
	}
	if m.URL != gateway.URL() {
		return fmt.Sprintf("%s's daemon was started for the gateway at %s", scienceName, m.URL)
	}
	return ""
}

// KeepClaudeScience starts the daemon on the gateway, while Claude Science
// is connected, whenever it isn't running: after a restart of the
// computer, or after it was stopped. It never stops one running — one
// uni-switch didn't start may be in the middle of a session — which Check
// tells as drift instead.
func KeepClaudeScience(ctx context.Context) {
	keepScienceOnce()
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			keepScienceOnce()
		}
	}
}

func keepScienceOnce() {
	if !scienceWired() {
		return
	}
	scienceMu.Lock()
	defer scienceMu.Unlock()
	bin := scienceBin()
	if bin == "" {
		return
	}
	if st, err := scienceState(bin); err != nil || st.Running {
		return
	}
	gw := gateway.URL()
	m, _ := readScienceMark()
	st, err := scienceServe(bin, gw, m.Port)
	if err != nil {
		log.Printf("%s: starting its daemon on uni-switch's gateway: %v", scienceName, err)
		return
	}
	if err := writeScienceMark(scienceMark{URL: gw, PID: st.PID, Port: st.Port}); err != nil {
		log.Printf("%s: %v", scienceName, err)
	}
}

// ScienceLaunch is the command that starts the daemon at bin on the
// gateway at gw, in the shell of this system.
func ScienceLaunch(bin, gw string) string {
	if runtime.GOOS == "windows" {
		return `& '` + strings.ReplaceAll(bin, "'", "''") + `' stop; $env:` + scienceVar + `="` + gw + `"; & '` + strings.ReplaceAll(bin, "'", "''") + `' serve --detached`
	}
	q := "'" + strings.ReplaceAll(bin, "'", `'\''`) + "'"
	return q + " stop; " + scienceVar + "=" + gw + " " + q + " serve --detached"
}

func claudeScience() *Agent {
	return &Agent{
		ID: ScienceID, Name: scienceName, Icon: "claude-color", Aliases: []string{"operon", "science"},
		// its picker lists the gateway's /v1/models as it is shown them
		// (gateway/science.go), which the row's Models button picks
		ListsModels: true,
		detect:      func() bool { return scienceBin() != "" },
		Launch: func() string {
			if bin := scienceBin(); bin != "" && scienceWired() {
				return ScienceLaunch(bin, gateway.URL())
			}
			return ""
		},
		Fields: []Field{{
			Key: "provider", Label: "provider",
			Get: func() string {
				if scienceWired() {
					return uniSwitchID
				}
				return ""
			},
			Set: func(v string) error { return scienceOn(v != "") },
			Options: func(map[string]string) []Option {
				return []Option{{Value: uniSwitchID, Label: "uni-switch", Icon: "uni-switch",
					Note: "uni-switch restarts its daemon with ANTHROPIC_BASE_URL set to the gateway; a session running in it is cut off"}}
			},
		}},
		Check: scienceCheck,
		Notice: func() string {
			if !scienceWired() {
				return ""
			}
			return noticeScience.say("agent", scienceName)
		},
	}
}

// what Claude Science says after a change (notice.go)
var noticeScience = newNotice("{agent}'s daemon runs on uni-switch's gateway: open {agent} again (reload its window if it is open). It still needs you signed in with your Claude account, which uni-switch never sends on; pick a model of uni-switch's in its model menu.")
