// Package libc tells which C library this Linux runs on, for the
// downloads that come in a glibc build and a musl one: Claude Code's and
// Bun's. A glibc build names glibc's loader (/lib64/ld-linux-x86-64.so.2)
// as its interpreter, which a musl Linux such as Alpine doesn't have, so
// the kernel can't start it and exec says "no such file or directory"
// (#1877).
package libc

import (
	"os"
	"runtime"
)

// muslLoaders are the files that tell a musl Linux, as Anthropic's
// installer tells one: musl's libc, under the name Alpine gives it.
var muslLoaders = []string{"/lib/libc.musl-x86_64.so.1", "/lib/libc.musl-aarch64.so.1"}

// Musl is whether this is a Linux whose C library is musl (Alpine). It is
// false on every other system.
func Musl() bool {
	return runtime.GOOS == "linux" && muslAt(muslLoaders)
}

func muslAt(paths []string) bool {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}
