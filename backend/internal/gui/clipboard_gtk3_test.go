//go:build linux && cgo && gtk3 && !nogui

package gui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestClipboardReadBack builds testdata/clipboard_probe.c with the copy and
// the read back the app runs (clipboard_gtk3.h) and copies in a real GTK 3
// window on the Wayland compositor the test runs under. uni-switch's copy is
// read back whole. Wails' copy (CLIPBOARD, then PRIMARY) is read back empty
// where the compositor has no primary selection, as on #1881's KDE Plasma
// with middle-click paste off, and the read back then says no copy was
// made. It needs a Wayland session that gives a new window the focus
// (KWin, with kwinrc [Wayland] EnablePrimarySelection=false for the
// second case).
func TestClipboardReadBack(t *testing.T) {
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("needs a Wayland session")
	}
	flags, err := exec.Command("pkg-config", "--cflags", "--libs", "gtk+-3.0", "wayland-client").Output()
	if err != nil {
		t.Skip("no GTK 3 / wayland-client headers:", err)
	}
	here, _ := filepath.Abs(".")
	bin := filepath.Join(t.TempDir(), "probe")
	args := append([]string{"testdata/clipboard_probe.c", "-I" + here, "-o", bin}, strings.Fields(string(flags))...)
	if out, err := exec.Command("cc", args...).CombinedOutput(); err != nil {
		t.Fatalf("build probe: %v\n%s", err, out)
	}
	run := func(t *testing.T, mode, text string) (primary, read int, held string, out string) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, mode, text)
		cmd.Env = append(os.Environ(), "GDK_BACKEND=wayland")
		b, err := cmd.CombinedOutput()
		out = string(b)
		t.Logf("%s: %s", mode, out)
		if err != nil {
			t.Fatalf("probe: %v", err)
		}
		primary, read = -2, -2
		for _, l := range strings.Split(out, "\n") {
			fmt.Sscanf(l, "primary=%d", &primary)
			if strings.HasPrefix(l, "read=") {
				var n int
				fmt.Sscanf(l, "read=%d len=%d", &read, &n)
				if i := strings.Index(l, " text="); i >= 0 {
					held = l[i+len(" text="):]
				}
			}
		}
		if read == -1 {
			t.Skip("the compositor sent the probe no selection (no focus?)")
		}
		return primary, read, held, out
	}
	text := "claude --resume 1881 «剪贴板»"
	t.Run("uni-switch", func(t *testing.T) {
		_, read, held, _ := run(t, "uni-switch", text)
		if !copyHeld(read, held, text) {
			t.Errorf("read back %d %q, want %q", read, held, text)
		}
	})
	t.Run("wails", func(t *testing.T) {
		primary, read, held, _ := run(t, "wails", text)
		if primary != 0 {
			t.Skip("the compositor has a primary selection: Wails' copy works there")
		}
		if read != 1 || held != "" {
			t.Errorf("read back %d %q, want 1 and an empty text (GTK closing the pipe)", read, held)
		}
		if copyHeld(read, held, text) {
			t.Error("an empty read back counts as a copy made")
		}
	})
	// #2039: a read back that left its data device alive in the
	// compositor broke GTK's connection on the next clipboard changes
	// ("Error reading events from display: Invalid argument") and the app
	// exited.
	t.Run("copies after a read back", func(t *testing.T) {
		_, read, held, out := run(t, "again", text)
		if !copyHeld(read, held, text) {
			t.Errorf("read back %d %q, want %q", read, held, text)
		}
		if !strings.Contains(out, "\nalive") {
			t.Errorf("the probe died after more copies:\n%s", out)
		}
	})
}
