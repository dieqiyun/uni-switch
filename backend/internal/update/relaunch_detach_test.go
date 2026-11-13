//go:build !windows

package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// jobMarkerEnv, when set, makes TestRelaunchOutlivesItsJobsGroup's binary
// the app being relaunched: it starts the helper and exits.
const jobMarkerEnv = "UNI_SWITCH_TEST_RELAUNCH_MARKER"

// The shell that opens the new version once uni-switch has quit outlives the
// end of the launchd job uni-switch ran in. launchd kills the job's process
// group when its process exits: the Open at login agent without
// AbandonProcessGroup, and on the reporter's macOS 13 the application job
// LaunchServices makes for an app opened from the Dock or Finder (#2362).
// The shell, left in uni-switch's group, went with it: uni-switch quit and never
// came back. Here the test's child stands for uni-switch, leading a group of
// its own as a job's process does; once it exits the group is killed as
// launchd kills it, and the helper must still run its command.
func TestRelaunchOutlivesItsJobsGroup(t *testing.T) {
	if marker := os.Getenv(jobMarkerEnv); marker != "" {
		// a moment's sleep first, so the group is killed while the helper
		// is still waiting, as launchd's kill comes the moment uni-switch exits
		if err := runAfterExit(os.Getpid(), "sleep 1; echo opened > '"+marker+"'"); err != nil {
			os.Exit(3)
		}
		os.Exit(0)
	}
	marker := filepath.Join(t.TempDir(), "opened")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	app := exec.Command(self, "-test.run=^TestRelaunchOutlivesItsJobsGroup$")
	app.Env = append(os.Environ(), jobMarkerEnv+"="+marker)
	app.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if out, err := app.CombinedOutput(); err != nil {
		t.Fatalf("the app: %v\n%s", err, out)
	}
	// launchd ending the job: every process left in its group goes
	syscall.Kill(-app.Process.Pid, syscall.SIGKILL)
	for end := time.Now().Add(15 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if b, err := os.ReadFile(marker); err == nil && len(b) > 0 {
			return
		}
	}
	t.Fatal("the relaunch helper died with the job's process group: the new version was never opened")
}
