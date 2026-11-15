package libc

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// musl's libc at either name tells a musl Linux; neither there tells a
// glibc one.
func TestMuslAt(t *testing.T) {
	dir := t.TempDir()
	x64 := filepath.Join(dir, "libc.musl-x86_64.so.1")
	arm := filepath.Join(dir, "libc.musl-aarch64.so.1")
	if muslAt([]string{x64, arm}) {
		t.Fatal("musl with neither file there")
	}
	if err := os.WriteFile(arm, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !muslAt([]string{x64, arm}) {
		t.Fatal("not musl with musl's aarch64 libc there")
	}
}

// Only a Linux is ever musl.
func TestMuslOnlyOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" && Musl() {
		t.Fatalf("Musl() on %s", runtime.GOOS)
	}
}
