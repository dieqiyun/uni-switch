package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The Codex app's Restart restarts only the app-server of the app the row
// lists as left on the old list. It never quits the app and opens it
// again, which opened a new window (yoshi037070, 10-27).
func TestCodexAppReopen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip(staleOnWindows)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("CODEX_HOME", "")
	dir := filepath.Join(home, ".codex")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte("model_provider = \"uni-switch\"\n"), 0o600)

	const bundle = "/Applications/Codex.app"
	wasRunning, wasQuit, wasOpen, wasRestart := running, appQuit, appOpen, restartAppServer
	t.Cleanup(func() { running, appQuit, appOpen, restartAppServer = wasRunning, wasQuit, wasOpen, wasRestart })
	running = func(pat string) []runningProc {
		ps := []runningProc{
			{3 * time.Hour, bundle + "/Contents/MacOS/Codex"},
			{3 * time.Hour, bundle + "/Contents/Resources/codex app-server"},
		}
		var out []runningProc
		for _, p := range ps {
			if strings.Contains(pat, "codex") && strings.Contains(p.cmd, "codex ") || strings.Contains(pat, `Codex\.app/Contents/`) {
				out = append(out, p)
			}
		}
		return out
	}
	var did []string
	appQuit = func(_ context.Context, b string) error { did = append(did, "quit "+b); return nil }
	appOpen = func(_ context.Context, b string) error { did = append(did, "open "+b); return nil }
	restartAppServer = func(_ context.Context, b string) error { did = append(did, "restart "+b); return nil }
	a := codex(home)

	if err := reopenApp(context.Background(), a, "/Applications/Other.app"); err == nil || len(did) > 0 {
		t.Fatalf("an app not listed: %v, did %v", err, did)
	}
	if err := reopenApp(context.Background(), a, bundle); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(did, ", "); got != "restart "+bundle {
		t.Fatalf("did %q, want only its app-server restarted", got)
	}

	// a restart that fails is said
	did = nil
	restartAppServer = func(context.Context, string) error { return errors.New("didn't end") }
	if err := reopenApp(context.Background(), a, bundle); err == nil || len(did) > 0 {
		t.Fatalf("failed restart: %v, did %v", err, did)
	}
}

// The Restart on Codex's "the Codex app is still signed in as" notice says
// what went wrong in a notice the GUI translates, and an error it doesn't
// know (a failed listing) is "couldn't tell", never success.
func TestRestartSwitchedCodexAppSays(t *testing.T) {
	old := restartSwitchedApp
	t.Cleanup(func() { restartSwitchedApp = old })
	for _, c := range []struct {
		err  error
		want notice
	}{
		{provider.ErrCodexAppGone, noticeSwitchAppGone},
		{provider.ErrCodexAppNoEnd, noticeSwitchAppNoEnd},
		{provider.ErrCodexAppNotBack, noticeSwitchAppNotBack},
		{errors.New("ps: timed out"), noticeSwitchAppUnknown},
	} {
		restartSwitchedApp = func(context.Context) error { return c.err }
		err := RestartSwitchedCodexApp(context.Background())
		if err == nil || err.Error() != c.want.say("agent", "Codex") {
			t.Errorf("%v: said %v, want %q", c.err, err, c.want.say("agent", "Codex"))
		}
	}
	restartSwitchedApp = func(context.Context) error { return nil }
	if err := RestartSwitchedCodexApp(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// When the Windows Codex app starts no app-server in place of the one
// uni-switch ended (its stdout held open by a process that one started), it
// can't send till it is quit and opened again (蛋仔 on Discord, 10-30):
// uni-switch does that, to the OpenAI.Codex package's ChatGPT.exe only, by
// its AppUserModelID. macOS isn't reopened unasked.
func TestCodexAppReopenedOnWindows(t *testing.T) {
	restartSandbox(t)
	restartOnWindows = true
	appCloseWait, appBackWait, appTrayGrace = time.Second, time.Second, 10*time.Millisecond
	const image = `C:\Program Files\WindowsApps\OpenAI.Codex_26.1021.3705.0_x64__2p2nqsd0c76g0\app\ChatGPT.exe`
	start := time.Now().Add(-time.Hour)
	alive := true
	winProcs = func(name string) []winProc {
		if !strings.EqualFold(name, "ChatGPT.exe") {
			return nil
		}
		// the ChatGPT chat app beside it, always running
		ps := []winProc{{pid: 50, parent: 4, image: `C:\Program Files\WindowsApps\OpenAI.ChatGPT-Desktop_1.2025.1.0_x64__2p2nqsd0c76g0\app\ChatGPT.exe`, start: start}}
		if alive {
			ps = append(ps,
				winProc{pid: 10, parent: 4, image: image, start: start, aumid: "OpenAI.Codex_2p2nqsd0c76g0!App"},
				winProc{pid: 11, parent: 10, image: image, start: start.Add(time.Second)})
		}
		return ps
	}
	winShown = func([]int) bool { return false } // in the tray
	var did []string
	winClose = func(pids []int) error { did = append(did, fmt.Sprint("close ", pids)); return nil }
	winEnd = func(pid int) error { did = append(did, fmt.Sprint("end ", pid)); alive = false; return nil }
	winLaunch = func(p winProc) error { did = append(did, "open "+p.aumid); alive = true; return nil }

	if open, known := codexAppOpen(context.Background()); !open || !known {
		t.Fatalf("open %v, known %v", open, known)
	}
	if err := codexAppReopen(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"close [10 11]", "end 10", "open OpenAI.Codex_2p2nqsd0c76g0!App"}; !slices.Equal(did, want) {
		t.Fatalf("did %q, want %q", did, want)
	}

	// quit meanwhile: nothing to do, it starts one as it opens
	alive, did = false, nil
	if open, known := codexAppOpen(context.Background()); open || !known {
		t.Fatalf("quit: open %v, known %v", open, known)
	}
	if err := codexAppReopen(context.Background()); err != nil || did != nil {
		t.Fatalf("quit: %v, did %q", err, did)
	}

	restartOnWindows = false
	alive = true
	if err := codexAppReopen(context.Background()); err == nil || did != nil {
		t.Fatalf("not Windows: %v, did %q", err, did)
	}
}
