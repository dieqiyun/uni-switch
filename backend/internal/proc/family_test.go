package proc

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseFamily(t *testing.T) {
	got := parseFamily("  101     1 /usr/bin/node  /x/npx-cli.js -y tavily-mcp\n  7 0\nnope\n 102 101 node index.js\n")
	want := []Kin{{101, 1, "/usr/bin/node  /x/npx-cli.js -y tavily-mcp"}, {7, 0, ""}, {102, 101, "node index.js"}}
	if !slices.Equal(got, want) {
		t.Fatalf("%+v", got)
	}
}

// A process under one picked goes with it, and uni-switch and what it runs
// under are never picked, whatever their command lines say.
func TestPicks(t *testing.T) {
	ps := []Kin{
		{1, 0, "init x"},         // under uni-switch
		{10, 1, "codex x"},       // the agent
		{11, 10, "cmd /c npx x"}, // the wrapper: picked
		{12, 11, "npx-cli x"},    // under it: ends with it
		{13, 12, "node index.js"},
		{20, 1, "npx-cli x"}, // its wrapper gone: picked
		{30, 1, "shell x"},   // runs uni-switch
		{31, 30, "uni-switch x"},
		{40, 40, "loop x"}, // its own parent
	}
	match := func(a string) bool { return strings.HasSuffix(a, " x") }
	if got := picks(ps, match, 31); !slices.Equal(got, []int{10, 20, 40}) {
		t.Errorf("picks: %v", got)
	}
	if got := under(ps, 10); !slices.Equal(got, []int{11, 12, 13}) {
		t.Errorf("under: %v", got)
	}
}

// A helper for TestEndTreesEndsWhatItStarted: the parent starts a child,
// and both wait.
func TestFamilyHelper(t *testing.T) {
	switch os.Getenv("UNI_SWITCH_FAMILY_HELPER") {
	case "parent":
		cmd := Command(os.Args[0], "-test.run=^TestFamilyHelper$")
		cmd.Env = append(os.Environ(), "UNI_SWITCH_FAMILY_HELPER=child")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(60 * time.Second)
		_ = cmd.Process.Kill()
	case "child":
		time.Sleep(60 * time.Second)
	}
}

// EndTrees ends the process its match picks, and what that process
// started, whose own command line names nothing it picks: npx's server
// under npx (#3885).
func TestEndTreesEndsWhatItStarted(t *testing.T) {
	mark := fmt.Sprintf("family-%d-%d", os.Getpid(), time.Now().UnixNano())
	cmd := Command(os.Args[0], "-test.run=^TestFamilyHelper$", mark)
	cmd.Env = append(os.Environ(), "UNI_SWITCH_FAMILY_HELPER=parent")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	child := 0
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		if child > 0 {
			_ = kill(child)
		}
		<-done
	})
	alive := func(pid int) bool {
		ps, err := family(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range ps {
			if p.PID == pid && !strings.Contains(p.Args, "<defunct>") {
				return true
			}
			if pid == 0 && p.PPID == cmd.Process.Pid {
				child = p.PID
				return true
			}
		}
		return false
	}
	for end := time.Now().Add(20 * time.Second); !alive(0); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal("the helper's child never started")
		}
	}
	n, err := EndTrees(context.Background(), func(args string) bool { return strings.HasSuffix(args, " "+mark) })
	if err != nil || n != 1 {
		t.Fatalf("EndTrees = %d, %v; want 1", n, err)
	}
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the picked process runs 20s after EndTrees")
	}
	for end := time.Now().Add(20 * time.Second); alive(child); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal("the process it started runs 20s after EndTrees")
		}
	}
}
