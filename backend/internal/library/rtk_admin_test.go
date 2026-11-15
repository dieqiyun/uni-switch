package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
	"github.com/dieqiyun/uni-switch/backend/internal/update"
)

// elevation fakes Windows' administrator prompt: writable is whether the
// user can change rtk's folder, answer what running c through the prompt
// gives. It returns the commands that went through it.
func elevation(t *testing.T, writable bool, answer func(c []string) ([]byte, error)) *[][]string {
	t.Helper()
	var ran [][]string
	e, w, r := rtkElevates, rtkWritable, runElevated
	rtkElevates = true
	rtkWritable = func(string) bool { return writable }
	runElevated = func(_ context.Context, _ string, env []string, c ...string) ([]byte, error) {
		if !hasEnv(env, "NONINTERACTIVE=1") {
			t.Errorf("elevated %v without NONINTERACTIVE=1", c)
		}
		ran = append(ran, c)
		return answer(c)
	}
	t.Cleanup(func() { rtkElevates, rtkWritable, runElevated = e, w, r })
	return &ran
}

func hasEnv(env []string, kv string) bool {
	for _, e := range env {
		if e == kv {
			return true
		}
	}
	return false
}

// 新v学员 on Discord: "RTK and other actions that need elevation all
// fail". RTK winget installed for every user of the computer (under
// Program Files) can only be upgraded by an administrator, and uni-switch runs
// as the user, so Upgrade failed. Upgrade now runs winget through Windows'
// administrator prompt when only an administrator can change rtk's folder,
// says so on the button first, and a prompt declined or not to be had
// says why and what to do instead.
func TestRTKUpgradeAsksForAdmin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fakes are shell scripts")
	}
	h := sandbox(t)
	latestServer(t, "v0.51.0")
	dir := filepath.Join(h, "Program Files", "WinGet", "Links")
	ver := filepath.Join(dir, "VERSION")
	testenv.Program(t, filepath.Join(dir, "rtk"), strings.ReplaceAll(versionedRTK, "VERSION", ver))
	plain := filepath.Join(h, "winget-ran")
	tools := filepath.Join(h, "tools")
	testenv.Program(t, filepath.Join(tools, "winget"), `#!/bin/sh
: > "`+plain+`"; echo 0.51.0 > "`+ver+`"; echo "Successfully installed"
`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+tools)
	up := upgraderOf
	upgraderOf = func(string) []string {
		return []string{"winget", "upgrade", "--id", "rtk-ai.rtk", "--exact", "--silent"}
	}
	t.Cleanup(func() { upgraderOf = up })
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	reset := func() {
		write(t, ver, "0.50.0")
		os.Remove(plain)
		forgetChannels()
	}

	var answer func() ([]byte, error)
	ran := elevation(t, false, func(c []string) ([]byte, error) { return answer() })

	// the card says, before the click, that Windows will ask
	reset()
	if v := ReadRTK(); v.Admin != real {
		t.Fatalf("admin folder %q, want %q", v.Admin, real)
	}

	// allowed: winget ran as administrator, never as the user
	reset()
	answer = func() ([]byte, error) {
		write(t, ver, "0.51.0")
		return []byte("Successfully installed"), nil
	}
	v, err := UpgradeRTK()
	if err != nil || v.Version != "0.51.0" {
		t.Fatalf("allowed: %+v, %v", v, err)
	}
	if len(*ran) != 1 || (*ran)[0][0] != "winget" || (*ran)[0][1] != "upgrade" {
		t.Fatalf("through the prompt: %v", *ran)
	}
	if _, err := os.Stat(plain); err == nil {
		t.Fatal("winget ran as the user too")
	}

	// declined: why, and what to do, with no npm advice
	reset()
	answer = func() ([]byte, error) { return nil, update.ErrCanceled }
	_, err = UpgradeRTK()
	var ae *agent.AdminError
	if !errors.As(err, &ae) || !ae.Canceled || ae.Via != "winget" || ae.Prefix != real || ae.Agent != "RTK" {
		t.Fatalf("declined: %#v", err)
	}
	if m := err.Error(); !strings.Contains(m, "was declined") || !strings.Contains(m, "terminal opened as administrator") || !strings.Contains(m, "winget upgrade") || strings.Contains(m, "npm") {
		t.Fatalf("declined says %q", m)
	}

	// no prompt to be had: says why
	reset()
	answer = func() ([]byte, error) { return nil, fmt.Errorf("%w: ShellExecuteEx: no", update.ErrNoAdmin) }
	_, err = UpgradeRTK()
	if !errors.As(err, &ae) || ae.Canceled || !strings.Contains(err.Error(), "couldn't ask Windows for one (no administrator prompt: ShellExecuteEx: no)") {
		t.Fatalf("no prompt: %v", err)
	}

	// winget as administrator has nothing newer: no failure
	reset()
	answer = func() ([]byte, error) {
		return []byte("No available upgrade found."), &update.ExitError{Code: wingetNoUpgrade}
	}
	if v, err := UpgradeRTK(); err != nil || !strings.Contains(v.Note, "winget") {
		t.Fatalf("no upgrade: %+v, %v", v, err)
	}

	// winget as administrator fails: said as any winget failure
	reset()
	answer = func() ([]byte, error) { return nil, &update.ExitError{Code: 0x8A150101} }
	if _, err := UpgradeRTK(); err == nil || !strings.Contains(err.Error(), "exit code 0x8A150101") || !strings.Contains(err.Error(), "RTK is in use") {
		t.Fatalf("failed as administrator: %v", err)
	}

	// a folder the user can change: winget runs as the user, no prompt
	*ran = nil
	rtkWritable = func(string) bool { return true }
	reset()
	if v := ReadRTK(); v.Admin != "" {
		t.Fatalf("writable folder is admin's: %q", v.Admin)
	}
	if v, err := UpgradeRTK(); err != nil || v.Version != "0.51.0" {
		t.Fatalf("as the user: %+v, %v", v, err)
	}
	if _, err := os.Stat(plain); err != nil || len(*ran) != 0 {
		t.Fatalf("as the user: plain %v, elevated %v", err, *ran)
	}

	// no administrator prompt here (not Windows), or not winget: as before
	rtkWritable = func(string) bool { return false }
	rtkElevates = false
	if f := rtkAdminFolder([]string{"winget", "upgrade"}, filepath.Join(dir, "rtk")); f != "" {
		t.Fatalf("no prompt here, but %q", f)
	}
	rtkElevates = true
	if f := rtkAdminFolder([]string{"brew", "upgrade", "rtk"}, filepath.Join(dir, "rtk")); f != "" {
		t.Fatalf("brew, but %q", f)
	}
}

// Install has no rtk folder to look at first: winget installing for every
// user answers that it needs an administrator, and is run again through
// Windows' administrator prompt. Any other installer's failure stays one.
func TestRTKInstallRetriesAsAdmin(t *testing.T) {
	needs := func(c []string, _ time.Duration, _ ...string) (string, error) {
		return "", installFailed(c, "", int(wingetNeedsAdmin), errors.New("exit status"))
	}
	p := runPlain
	runPlain = needs
	t.Cleanup(func() { runPlain = p })
	ran := elevation(t, true, func([]string) ([]byte, error) { return []byte("Successfully installed"), nil })

	winget := []string{"winget", "install", "--id", "rtk-ai.rtk"}
	if _, err := runRTKInstaller(winget, time.Minute, ""); err != nil || len(*ran) != 1 {
		t.Fatalf("winget install: %v, elevated %v", err, *ran)
	}
	*ran = nil
	if _, err := runRTKInstaller([]string{"brew", "install", "rtk"}, time.Minute, ""); err == nil || len(*ran) != 0 {
		t.Fatalf("brew install: %v, elevated %v", err, *ran)
	}

	// no prompt here: winget's own answer, said plainly
	rtkElevates = false
	_, err := runRTKInstaller(winget, time.Minute, "")
	if err == nil || !strings.Contains(err.Error(), "0x8A150019): winget needs an administrator") || len(*ran) != 0 {
		t.Fatalf("no prompt: %v, elevated %v", err, *ran)
	}
}
