package cli

import (
	"os"
	"testing"
)

// swapStdin sets os.Stdin to a pipe holding text, after package init, and
// touches nothing else: it is what the Windows desktop build's init does
// when it reopens the terminal's CONIN$ (console_windows.go).
func swapStdin(t *testing.T, text string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString(text)
	w.Close()
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = old
		r.Close()
	})
}
