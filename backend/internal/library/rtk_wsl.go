package library

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/wslrun"
)

// RTK for an agent in a WSL distro (lex on Discord: Claude Code in WSL
// couldn't have the RTK of a uni-switch on Windows). Its hook runs in the
// distro, where a Windows rtk.exe is no command, so it is the distro's own
// rtk that is installed (rtk's own Linux script, run there), found, and
// run for rtk init, with its files under the distro's home as uni-switch
// opens it (\\wsl.localhost\<distro>\home\<user>). A distro that isn't
// running is never started for it: its agents are listed asleep.

// RTKDistro is the rtk of one WSL distro an agent listed lives in.
type RTKDistro struct {
	Name string `json:"name"`
	// Path is rtk's inside the distro, "" when it isn't installed there
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
	// OffPath: found where its installer puts it, but not on the PATH of
	// the distro's shells, which its hooks run rtk by
	OffPath bool `json:"offPath,omitempty"`
	// Install is the command Install RTK runs in the distro
	Install string `json:"install,omitempty"`
	// Asleep: the distro isn't running, and isn't looked in
	Asleep bool `json:"asleep,omitempty"`
}

// rtkKind is the rtkSpecs (and rtkNoHook) key of agent id: the id itself,
// or one in WSL's (claude@wsl:Ubuntu) before its @wsl:.
func rtkKind(id string) string {
	k, _, _ := strings.Cut(id, "@wsl:")
	return k
}

// rtkHome is the home rtk's installer writes under for a: the distro's for
// one in WSL, as uni-switch opens it; this machine's otherwise.
func rtkHome(a *agent.Agent) string {
	if a != nil && a.WSL != "" {
		return a.Home
	}
	return home()
}

// rtkAt is a path under rtkHome(a): in a distro, opened where a link
// there leads (agent.WSLOpen, #4502), as nothing below one can be opened
// through \\wsl.localhost; as it is when the link couldn't be resolved,
// which rtkInstall says.
func rtkAt(a *agent.Agent, elem ...string) string {
	p := filepath.Join(append([]string{rtkHome(a)}, elem...)...)
	if a == nil || a.WSL == "" {
		return p
	}
	if o, err := agent.WSLOpen(a, p); err == nil {
		return o
	}
	return p
}

// rtkClaudeDir is Claude Code's folder for a: ~/.claude in a distro, whose
// CLAUDE_CONFIG_DIR uni-switch can't read (as wslTargetOf).
func rtkClaudeDir(a *agent.Agent) string {
	if a != nil && a.WSL != "" {
		return rtkAt(a, ".claude")
	}
	return claudeDir()
}

// rtkCodexDir is Codex's folder for a: ~/.codex in a distro.
func rtkCodexDir(a *agent.Agent) string {
	if a != nil && a.WSL != "" {
		return rtkAt(a, ".codex")
	}
	return codexDir()
}

// rtkVersionOf is the version of the rtk a's hook runs: its distro's for
// one in WSL.
func rtkVersionOf(a *agent.Agent) string {
	if a != nil && a.WSL != "" {
		return rtkDistroOf(a.WSL, false).Version
	}
	return rtkVersion()
}

// openCodeVersionOf is the version of a's OpenCode: for one in WSL what the
// distro's probe found, "" when it didn't ask.
func openCodeVersionOf(a *agent.Agent) string {
	if a != nil && a.WSL != "" {
		return a.InstalledVersion()
	}
	return openCodeVersion()
}

// rtkWSLLive is whether a's distro runs now and its files can be opened
// without starting it.
func rtkWSLLive(a *agent.Agent) bool {
	return a.Home != "" && agent.WSLRunning(a.WSL)
}

// rtkWSLRun runs wsl.exe with args; a var for tests.
var rtkWSLRun = wslrun.Run

// rtkWSLProbe prints where rtk is in the distro: on the PATH of a login
// shell, or an interactive one's (where .bashrc puts it), "on:1"; else
// where its installers put it, "on:0". A Windows rtk.exe seen through
// /mnt isn't the distro's.
const rtkWSLProbe = `p=$(command -v rtk 2>/dev/null); ` +
	`case "$p" in /*) ;; *) p=$(bash -ic 'command -v rtk' 2>/dev/null </dev/null | tail -n1);; esac; ` +
	`on=1; case "$p" in /mnt/*|"") p=""; on=0; ` +
	`for c in "$HOME/.local/bin/rtk" "$HOME/.cargo/bin/rtk"; do [ -x "$c" ] && { p=$c; break; }; done;; /*) ;; *) p=""; on=0;; esac; ` +
	`[ -n "$p" ] || exit 0; echo "bin:$p"; echo "on:$on"; ` +
	`echo "version:$("$p" --version 2>/dev/null | head -n1)"`

// rtkWSLScript is rtk's own Linux installer, run in the distro: it puts rtk
// in ~/.local/bin. curl, or wget where a distro has no curl.
const rtkWSLScript = `set -e; u=` + rtkScript + `; ` +
	`if command -v curl >/dev/null 2>&1; then s=$(curl -fsSL "$u"); ` +
	`elif command -v wget >/dev/null 2>&1; then s=$(wget -qO- "$u"); ` +
	`else echo "neither curl nor wget is installed in this distro" >&2; exit 1; fi; ` +
	`printf '%s\n' "$s" | sh`

// rtkWSLShown is how Install RTK names what it runs in a distro.
const rtkWSLShown = "curl -fsSL " + rtkScript + " | sh"

var rtkDistros_ struct {
	sync.Mutex
	at  map[string]time.Time
	got map[string]RTKDistro
}

// rtkDistroAge is how long a distro's answer is kept: an rtk installed in
// it by hand is seen within it.
const rtkDistroAge = 15 * time.Second

// rtkDistroOf is the rtk of distro, which is asked (never started) unless
// it answered within rtkDistroAge; fresh asks it again.
func rtkDistroOf(distro string, fresh bool) RTKDistro {
	d := RTKDistro{Name: distro}
	if !agent.WSLRunning(distro) {
		d.Asleep = true
		return d
	}
	rtkDistros_.Lock()
	at, ok := rtkDistros_.at[distro]
	kept := rtkDistros_.got[distro]
	rtkDistros_.Unlock()
	if ok && !fresh && time.Since(at) < rtkDistroAge {
		return kept
	}
	// asked with no lock held: a distro slow to answer holds no other read
	out, err := rtkWSLRun(30*time.Second, "-d", distro, "-e", "sh", "-lc", rtkWSLProbe)
	if err != nil {
		// not known: not kept, and not offered an install either
		return d
	}
	for _, l := range strings.Split(string(out), "\n") {
		k, val, _ := strings.Cut(strings.TrimSpace(l), ":")
		switch val = strings.TrimSpace(val); k {
		case "bin":
			d.Path = val
		case "on":
			d.OffPath = val == "0"
		case "version":
			if m := semver.FindStringSubmatch(val); m != nil {
				d.Version = m[1]
			}
		}
	}
	if !strings.HasPrefix(d.Path, "/") {
		d.Path, d.OffPath, d.Version = "", false, ""
	}
	if d.Path == "" {
		d.Install = rtkWSLShown
	}
	rtkDistros_.Lock()
	defer rtkDistros_.Unlock()
	if rtkDistros_.at == nil {
		rtkDistros_.at, rtkDistros_.got = map[string]time.Time{}, map[string]RTKDistro{}
	}
	rtkDistros_.at[distro], rtkDistros_.got[distro] = time.Now(), d
	return d
}

// rtkDistros is the rtk of each distro an agent of list lives in.
func rtkDistros(list []RTKAgent) []RTKDistro {
	var names []string
	for _, a := range list {
		if a.WSL != "" && !a.NoHook && !slices.Contains(names, a.WSL) {
			names = append(names, a.WSL)
		}
	}
	var out []RTKDistro
	for _, n := range names {
		out = append(out, rtkDistroOf(n, false))
	}
	return out
}

// shq is s quoted for sh.
func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// rtkInstallWSL runs rtk's installer in a's distro, by the distro's rtk
// (bin, its path there). It runs in a login shell, with what that gives the
// distro's agents; HOME is the distro's, so it writes under the home uni-switch
// reads a's files at. Claude Code is pointed at a folder of its own made
// and taken away there, as on this machine. An agent's folder variable
// (HERMES_HOME) isn't set: the distro's own, if any, is what its agent
// reads too.
func rtkInstallWSL(bin string, a *agent.Agent, sp rtkSpec) error {
	run, args := rtkInitWSL(bin, sp)
	out, err := rtkWSLRun(time.Minute, "-d", a.WSL, "-e", "sh", "-lc", run)
	if err != nil {
		said := installerSaid(string(out)+"\n"+exitSaid(err), 3)
		return fmt.Errorf("rtk %s in WSL %s: %s", strings.Join(args, " "), a.WSL, said)
	}
	if !sp.has(a) {
		return fmt.Errorf("rtk's installer didn't add its hook to %s: %s", a.Name, installerSaid(string(out), 3))
	}
	return nil
}

// rtkInitWSL is the sh command that runs the distro's rtk (bin) for sp's
// agent, and rtk init's arguments.
func rtkInitWSL(bin string, sp rtkSpec) (string, []string) {
	args := append([]string{"init", "-g"}, sp.flags...)
	if sp.patch {
		args = append(args, "--auto-patch")
	}
	q := make([]string, len(args))
	for i, s := range args {
		q[i] = shq(s)
	}
	run := "PATH=" + shq(bin[:strings.LastIndex(bin, "/")]) + `:"$PATH" ` + shq(bin) + " " + strings.Join(q, " ") + " </dev/null 2>&1"
	if sp.withClaude {
		run = `t=$(mktemp -d) || exit 1; CLAUDE_CONFIG_DIR="$t" ` + run + `; s=$?; rm -rf "$t"; exit $s`
	}
	return run, args
}

// exitSaid is what a failed wsl.exe wrote to stderr, or the error.
func exitSaid(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) && len(ee.Stderr) > 0 {
		return string(ee.Stderr)
	}
	return err.Error()
}

// InstallRTKIn installs rtk in WSL distro with rtk's own Linux installer,
// run there, when the user asks for it. A distro that isn't running isn't
// started for it.
func InstallRTKIn(distro string) (*RTKView, error) {
	rtkMu.Lock()
	known := false
	for _, a := range rtkAgents(detected()) {
		known = known || strings.EqualFold(a.WSL, distro)
		if strings.EqualFold(a.WSL, distro) {
			distro = a.WSL
		}
	}
	if !known {
		rtkMu.Unlock()
		return nil, fmt.Errorf("no agent RTK has a hook for is in a WSL distro named %q", distro)
	}
	if !agent.WSLRunning(distro) {
		rtkMu.Unlock()
		return nil, fmt.Errorf("WSL %s isn't running, and uni-switch doesn't start it to install RTK: start the distro (open its terminal), then install again", distro)
	}
	if d := rtkDistroOf(distro, true); d.Path != "" {
		rtkMu.Unlock()
		return ReadRTK(), nil
	}
	out, err := rtkWSLRun(5*time.Minute, "-d", distro, "-e", "sh", "-lc", rtkWSLScript+" 2>&1")
	d := rtkDistroOf(distro, true)
	rtkMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("RTK's installer failed in WSL %s: %s", distro, installerSaid(string(out)+"\n"+exitSaid(err), 3))
	}
	if d.Path == "" {
		return nil, fmt.Errorf("RTK's installer ran in WSL %s, but rtk still isn't found there: %s", distro, installerSaid(string(out), 3))
	}
	return ReadRTK(), nil
}
