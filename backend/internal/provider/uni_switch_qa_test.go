package provider

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

func TestUniSwitchQAProcessIsolation(t *testing.T) {
	if !proc.Isolated() {
		t.Fatal("test must run in an isolated HOME")
	}
	t.Run("enumeration", func(t *testing.T) {
		ps, err := listProcesses(context.Background())
		if err != nil || len(ps) != 0 {
			t.Fatalf("host process enumeration was not blocked: %d, %v", len(ps), err)
		}
	})
	t.Run("restart", func(t *testing.T) {
		// Use only a child this test owns, so the regression is safe even
		// when the guard is missing from an ordinary build.
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := proc.Command(exe, "-test.run=^TestUniSwitchQAIsolationChild$")
		cmd.Env = append(os.Environ(), "UNI_SWITCH_ISOLATION_CHILD=1")
		cmd.Stderr = os.Stderr
		stdin, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			stdin.Close()
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			stdin.Close()
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() {
			cmd.Wait()
			close(done)
		}()
		t.Cleanup(func() {
			stdin.Close()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				cmd.Process.Kill()
				<-done
			}
		})
		ping := func() {
			t.Helper()
			echoed := make(chan error, 1)
			go func() {
				_, err := io.WriteString(stdin, "alive\n")
				if err == nil {
					buf := make([]byte, len("alive\n"))
					_, err = io.ReadFull(stdout, buf)
					if err == nil && string(buf) != "alive\n" {
						err = fmt.Errorf("unexpected child reply: %q", buf)
					}
				}
				echoed <- err
			}()
			select {
			case err := <-echoed:
				if err != nil {
					t.Fatalf("test-owned child did not answer: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("test-owned child did not answer")
			}
		}
		ping()
		if err := endCodexAppServer(cmd.Process.Pid); err == nil || !strings.Contains(err.Error(), "QA process isolation") {
			t.Errorf("host process restart was not blocked: %v", err)
		}
		ping()
	})
}

func TestUniSwitchQAIsolationChild(t *testing.T) {
	if os.Getenv("UNI_SWITCH_ISOLATION_CHILD") != "1" {
		return
	}
	// Stay alive until the parent closes stdin during cleanup.
	io.Copy(os.Stdout, os.Stdin)
}
