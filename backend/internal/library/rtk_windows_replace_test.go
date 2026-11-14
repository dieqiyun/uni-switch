package library

import (
	"os"
	"path/filepath"
	"testing"
)

// #2006: the replacement itself is what the bug was about — the standalone
// rtk.exe had no upgrade at all — and the tests above skip the successful
// one on Windows, where the fake rtk is a shell script. This one needs no
// runnable rtk: it is the swap, on whatever platform it runs, including
// the one the issue is about, where the old file is renamed aside rather
// than removed.
func TestRTKWindowsReleaseSwapsTheFile(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "rtk.exe")
	write(t, bin, "the rtk that was here")
	// what an earlier upgrade left when the rtk it moved aside was running
	write(t, bin+".old-123", "an older rtk")

	if err := replaceRTKBinary(bin, []byte("the new rtk")); err != nil {
		t.Fatal(err)
	}
	if got := read(t, bin); got != "the new rtk" {
		t.Fatalf("binary %q; want %q", got, "the new rtk")
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Fatalf("left files behind in %s: %v", dir, ents)
	}
}
