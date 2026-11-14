package provider

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// A move's lock left by a uni-switch killed mid-move (a hung move stopped by
// hand) holds nothing: the next move takes it, where it used to be
// refused as "another uni-switch is moving" for a quarter of an hour.
func TestMoveLockOfDeadUniSwitch(t *testing.T) {
	claudeHome(t)
	gone := exec.Command("true")
	if err := gone.Run(); err != nil {
		t.Skip(err)
	}
	p := migrationsPath() + ".lock"
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strconv.Itoa(gone.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockMoves()
	if err != nil {
		t.Fatalf("a dead uni-switch's lock: %v", err)
	}
	// one held by a uni-switch still running stays held
	if _, err := lockMoves(); err == nil {
		t.Fatal("a live lock was taken")
	}
	unlock()
}
