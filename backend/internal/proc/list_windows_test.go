package proc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The test binary finds itself by its name's start, in another case too,
// with the command line it was started with (#3467).
func TestListFindsItselfWithItsCommandLine(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	prefix := strings.ToUpper(strings.TrimSuffix(filepath.Base(exe), ".exe"))
	ps, err := List(context.Background(), prefix)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if p.PID == os.Getpid() {
			if !strings.Contains(p.Args, "-test.") {
				t.Fatalf("own command line = %q, want the test flags in it", p.Args)
			}
			return
		}
	}
	t.Fatalf("List(%q) = %v, without this process (%d)", prefix, ps, os.Getpid())
}

func TestListLeavesOthersOut(t *testing.T) {
	ps, err := List(context.Background(), "no-such-program-3467")
	if err != nil || len(ps) != 0 {
		t.Fatalf("List = %v, %v; want nothing", ps, err)
	}
}

func BenchmarkList(b *testing.B) {
	for b.Loop() {
		if _, err := List(context.Background(), "codex"); err != nil {
			b.Fatal(err)
		}
	}
}
