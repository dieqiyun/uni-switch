package provider

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// an installer that fails says so, and how to install it by hand: Devin's
// plugin signs in through the CLI, which is installed first
func TestSignInInstallFails(t *testing.T) {
	claudeHome(t)
	oldExe := DevinExecutable
	DevinExecutable = func() string { return "" }
	t.Cleanup(func() { DevinExecutable = oldExe })
	oldRun := runInstaller
	runInstaller = func(ctx context.Context, c agentCLI) ([]byte, error) {
		return []byte("downloading…\n\x1b[0;31mError: Failed to download\x1b[0m\n"), errors.New("exit status 1")
	}
	t.Cleanup(func() { runInstaller = oldRun })

	st, err := StartPluginSignIn("devin", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	st = waitPast(t, st.ID, "installing")
	if st.State != "failed" || !strings.Contains(st.Error, "Error: Failed to download") || !strings.Contains(st.Error, "install it yourself") {
		t.Fatalf("state %+v", st)
	}
}

// on Windows the vendor's one-liner reaches PowerShell on stdin, never on
// its command line: Microsoft Defender took "powershell … -Command irm
// https://static.devin.ai/cli/setup.ps1 | iex" for a Trojan
// (Trojan:Win32/Commando.A!ml) and stopped it before it ran, so Devin's
// sign-in failed with "fork/exec …\powershell.exe: Access is denied"
func TestWindowsInstallerNotOnCommandLine(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell's command line is Windows'")
	}
	c := agentCLI{Name: "Probe CLI", ps: "Write-Output ([Environment]::CommandLine); exit 7"}
	out, err := runInstaller(context.Background(), c)
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 7 {
		t.Fatalf("the installer's exit: %v (output %q)", err, out)
	}
	line := lastLine(out)
	if !strings.Contains(strings.ToLower(line), "powershell") {
		t.Fatalf("the installer didn't print PowerShell's command line: %q", out)
	}
	if strings.Contains(line, "Write-Output") {
		t.Fatalf("PowerShell's command line carries the installer: %s", line)
	}
}

// canceled while installing: the sign-in never starts
func TestSignInCanceledWhileInstalling(t *testing.T) {
	claudeHome(t)
	oldExe := DevinExecutable
	DevinExecutable = func() string { return "" }
	t.Cleanup(func() { DevinExecutable = oldExe })
	stopped := make(chan struct{})
	oldRun := runInstaller
	runInstaller = func(ctx context.Context, c agentCLI) ([]byte, error) {
		<-ctx.Done()
		close(stopped)
		return nil, ctx.Err()
	}
	t.Cleanup(func() { runInstaller = oldRun })

	st, _ := StartPluginSignIn("devin", 0, nil)
	CancelSignIn(st.ID)
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the installer went on")
	}
	if st, _ = SignInStatus(st.ID); st.State != "canceled" {
		t.Fatalf("state %+v", st)
	}
}

func waitPast(t *testing.T, id, state string) SignInState {
	t.Helper()
	for end := time.Now().Add(signInDeadline); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if st, _ := SignInStatus(id); st.State != state {
			return st
		}
	}
	t.Fatalf("still %s after %s", state, signInDeadline)
	return SignInState{}
}

// uni-switch's Docker image had bash and busybox's wget but no curl, which every
// vendor's one-liner downloads with: there the native installer is used
func TestShellInstallerNeedsCurl(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the shell installer is for macOS and Linux")
	}
	bin := t.TempDir()
	for _, p := range []string{"bash", "wget"} {
		testenv.Program(t, filepath.Join(bin, p), "#!/bin/sh\n")
	}
	t.Setenv("PATH", bin)
	if shellInstallerRuns() {
		t.Fatal("the shell installer runs with bash and wget, without curl")
	}
	testenv.Program(t, filepath.Join(bin, "curl"), "#!/bin/sh\n")
	if !shellInstallerRuns() {
		t.Fatal("the shell installer doesn't run with bash and curl")
	}
}
