package proc

import (
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// The tests run in a home of their own, never the user's real files.
// With UNI_SWITCH_PROC_RAN set, the test binary stands in for a program a
// probe asks its version (guiexe_windows_test.go): it notes that it was
// started there and answers.
//
// With UNI_SWITCH_PROC_SLEEP set, it runs until its stdin closes
// (listener_test.go).
func TestMain(m *testing.M) {
	if os.Getenv("UNI_SWITCH_PROC_SLEEP") != "" {
		io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	if ran := os.Getenv("UNI_SWITCH_PROC_RAN"); ran != "" {
		if f, err := os.OpenFile(ran, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintln(f, os.Args[0])
			f.Close()
		}
		fmt.Println("2.2.5")
		os.Exit(0)
	}
	testenv.Main(m)
}
