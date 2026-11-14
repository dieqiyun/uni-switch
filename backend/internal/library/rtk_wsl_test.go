package library

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// fakeDistroRTK stands in for wsl.exe -d <distro> -e sh -lc <script> on the
// distros of wslSandbox: the script runs here, with the distro's home as
// HOME and a PATH with no rtk on it, except RTK's installer, which isn't
// fetched (a test never reaches the network): it puts fakeRTK where the
// real one puts rtk, ~/.local/bin. It counts what ran in each distro.
func fakeDistroRTK(t *testing.T, home string) *sync.Map {
	t.Helper()
	ran := &sync.Map{}
	old := rtkWSLRun
	rtkWSLRun = func(_ time.Duration, args ...string) ([]byte, error) {
		if len(args) != 6 || args[0] != "-d" || args[2] != "-e" || args[3] != "sh" || args[4] != "-lc" {
			return nil, errors.New("unexpected wsl.exe " + strings.Join(args, " "))
		}
		n, _ := ran.LoadOrStore(args[1], new(int))
		*n.(*int)++
		if args[5] == rtkWSLScript+" 2>&1" {
			bin := filepath.Join(home, ".local", "bin", "rtk")
			if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
				return nil, err
			}
			return []byte("installed\n"), os.WriteFile(bin, []byte(fakeRTK), 0o755)
		}
		cmd := exec.Command("/bin/sh", "-c", args[5])
		cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
		return cmd.Output()
	}
	forget := func() {
		rtkDistros_.Lock()
		rtkDistros_.at, rtkDistros_.got = nil, nil
		rtkDistros_.Unlock()
	}
	forget()
	t.Cleanup(func() { rtkWSLRun = old; forget() })
	return ran
}

func runsIn(ran *sync.Map, distro string) int {
	if n, ok := ran.Load(distro); ok {
		return *n.(*int)
	}
	return 0
}

func rtkRow(t *testing.T, v *RTKView, id string) RTKAgent {
	t.Helper()
	for _, a := range v.Agents {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("%s isn't on the RTK page: %+v", id, v.Agents)
	return RTKAgent{}
}

// Claude Code in a WSL distro gets RTK (lex on Discord): listed, with the
// distro's own rtk, installed there by RTK's installer and run there for
// the hook, which lands in the distro's ~/.claude, not this machine's.
func TestRTKWSL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake distro runs sh")
	}
	h := wslSandbox(t)
	ran := fakeDistroRTK(t, h)
	const distro = "Ubuntu-24.04"
	hooked := func(dir string) bool {
		b, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
		return strings.Contains(string(b), "rtk hook claude")
	}

	v := ReadRTK()
	if a := rtkRow(t, v, wslClaude); a.WSL != distro || a.On || a.Asleep {
		t.Fatalf("WSL Claude Code's row: %+v", a)
	}
	if len(v.WSL) != 1 || v.WSL[0].Name != distro || v.WSL[0].Path != "" || v.WSL[0].Install != rtkWSLShown || v.WSL[0].Asleep {
		t.Fatalf("the distro's RTK, with none installed: %+v", v.WSL)
	}
	// no rtk in the distro: nothing to switch it on with
	if _, err := SetRTK(wslClaude, true); err == nil || !strings.Contains(err.Error(), "isn't installed in WSL "+distro) {
		t.Fatalf("switched on with no rtk in the distro: %v", err)
	}
	if _, err := InstallRTKIn("no-such-distro"); err == nil {
		t.Fatal("installed in a distro no agent is in")
	}
	// the distro's name as the user types it
	v, err := InstallRTKIn(strings.ToLower(distro))
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(h, ".local", "bin", "rtk")
	if d := v.WSL[0]; d.Path != bin || d.Version != "9.9.9" || !d.OffPath || d.Install != "" {
		t.Fatalf("the distro's RTK after the install: %+v", d)
	}
	if _, err := SetRTK(wslClaude, true); err != nil {
		t.Fatal(err)
	}
	if !hooked(filepath.Join(h, ".claude")) {
		t.Fatal("no hook in the distro's ~/.claude/settings.json")
	}
	if hooked(filepath.Join(os.Getenv("HOME"), ".claude")) {
		t.Fatal("the hook went into this machine's Claude Code")
	}
	if a := rtkRow(t, ReadRTK(), wslClaude); !a.On {
		t.Fatalf("switched on, yet the row is off: %+v", a)
	}
	// this machine's Claude Code is its own
	if a := rtkRow(t, ReadRTK(), "claude"); a.On || a.WSL != "" {
		t.Fatalf("this machine's Claude Code: %+v", a)
	}
	if _, err := SetRTK(wslClaude, false); err != nil {
		t.Fatal(err)
	}
	if hooked(filepath.Join(h, ".claude")) {
		t.Fatal("switched off, the hook is still there")
	}
	if runsIn(ran, distro) == 0 {
		t.Fatal("nothing ran in the distro")
	}
}

// A distro that isn't running is never started for RTK (#3896): its agents
// are listed asleep, and switching or installing says to start it.
func TestRTKWSLAsleep(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake distro runs sh")
	}
	h := wslSandbox(t)
	ran := fakeDistroRTK(t, h)
	const distro = "Ubuntu-24.04"
	testenv.Program(t, filepath.Join(h, ".local", "bin", "rtk"), fakeRTK)
	// seen while it ran, then stopped
	if a := rtkRow(t, ReadRTK(), wslClaude); a.Asleep {
		t.Fatalf("running, yet asleep: %+v", a)
	}
	agent.StopFakeWSL(distro)
	before := runsIn(ran, distro)
	v := ReadRTK()
	if a := rtkRow(t, v, wslClaude); !a.Asleep || a.On || a.WSL != distro {
		t.Fatalf("a stopped distro's Claude Code: %+v", a)
	}
	if len(v.WSL) != 1 || !v.WSL[0].Asleep || v.WSL[0].Path != "" {
		t.Fatalf("a stopped distro's RTK: %+v", v.WSL)
	}
	if _, err := SetRTK(wslClaude, true); err == nil || !strings.Contains(err.Error(), "isn't running") {
		t.Fatalf("switched in a stopped distro: %v", err)
	}
	if _, err := InstallRTKIn(distro); err == nil || !strings.Contains(err.Error(), "isn't running") {
		t.Fatalf("installed in a stopped distro: %v", err)
	}
	if n := runsIn(ran, distro) - before; n != 0 {
		t.Fatalf("%d commands ran in the stopped distro, each starting it", n)
	}
}
