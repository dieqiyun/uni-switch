package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func init() {
	// The fake installer records the Win32 command line and exits before tests.
	if path := os.Getenv("UNI_SWITCH_TEST_NSIS_COMMAND_LINE"); path != "" {
		line := windows.UTF16PtrToString(windows.GetCommandLine())
		if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
}

func TestWindowsInstallerKeepsDirectoryWithSpaces(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	fake := filepath.Join(root, "fake installer 中文.exe")
	if err := os.WriteFile(fake, data, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"uni-switch", "uni switch", "uni switch 中文"} {
		t.Run(name, func(t *testing.T) {
			directory := filepath.Join(root, name)
			record := filepath.Join(t.TempDir(), "command-line.txt")
			cmd := windowsInstallerCommand(fake, directory)
			cmd.Env = append(os.Environ(), "UNI_SWITCH_TEST_NSIS_COMMAND_LINE="+record)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("fake installer: %v: %s", err, out)
			}
			line, err := os.ReadFile(record)
			if err != nil {
				t.Fatal(err)
			}
			// Match NSIS exehead/Main.c: the space before /D= and the entire
			// remaining directory are significant, including absence of quotes.
			_, actual, found := strings.Cut(string(line), " /D=")
			if !found || actual != directory {
				t.Fatalf("NSIS cannot retain install directory %q from command line %q", directory, line)
			}
		})
	}
}
