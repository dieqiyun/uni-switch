//go:build !windows

package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOpenLodyRunsItsDesktopEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	data := filepath.Join(home, "data")
	sys := filepath.Join(home, "sys")
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_DATA_DIRS", sys)
	oldOS, oldStart := lodyGOOS, startLody
	t.Cleanup(func() { lodyGOOS, startLody = oldOS, oldStart })
	lodyGOOS = "linux"
	var ran []string
	startErr := error(nil)
	startLody = func(cmd *exec.Cmd) error {
		if startErr == nil {
			ran = cmd.Args
		}
		return startErr
	}
	var opened string
	open := func(u string) error { opened = u; return nil }
	link := LodyLink("http://127.0.0.1:3425")

	program := func(path string) string {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	entry := func(dir, name, execLine string) string {
		p := filepath.Join(dir, "applications", name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		body := "[Desktop Entry]\nName=Lody\nExec=" + execLine + "\nTerminal=false\nType=Application\nMimeType=x-scheme-handler/ai.lody.stable;x-scheme-handler/lody;\n"
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	try := func(want []string) {
		t.Helper()
		ran, opened = nil, ""
		if err := OpenLody(link, open); err != nil {
			t.Fatal(err)
		}
		if want == nil {
			if ran != nil || opened != link {
				t.Fatalf("ran %q and opened %q, want the link opened", ran, opened)
			}
			return
		}
		if !reflect.DeepEqual(ran, want) || opened != "" {
			t.Fatalf("ran %q and opened %q, want %q run", ran, opened, want)
		}
	}

	try(nil) // no Lody entry: opened as before

	appImage := program(filepath.Join(home, "Apps $1", `Lody "x".AppImage`))
	stable := entry(data, "ai.lody.desktop.desktop", `"`+strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`", "%", "%%").Replace(appImage)+`" "--no-sandbox" %U`)
	try([]string{appImage, "--no-sandbox", link})

	// the .deb's entry, under XDG_DATA_DIRS, counts only after the user's own
	deb := program(filepath.Join(home, "opt", "Lody", "lodyDesktop"))
	entry(sys, "ai.lody.desktop.desktop", deb+" %U")
	try([]string{appImage, "--no-sandbox", link})
	// an AppImage deleted since Lody wrote its entry runs nothing, and the
	// system's entry of that name doesn't stand in for it
	if err := os.Remove(appImage); err != nil {
		t.Fatal(err)
	}
	try(nil)
	if err := os.Remove(stable); err != nil {
		t.Fatal(err)
	}
	try([]string{deb, link})

	// Nightly's, with an Exec that takes no URL: it goes last
	if err := os.RemoveAll(filepath.Join(sys, "applications")); err != nil {
		t.Fatal(err)
	}
	nightly := program(filepath.Join(home, "nightly", "lody"))
	entry(data, "ai.lody.desktop.nightly.desktop", nightly+" --no-sandbox")
	try([]string{nightly, "--no-sandbox", link})

	// a Lody that won't start: the link is opened as before
	startErr = os.ErrPermission
	try(nil)
	startErr = nil

	// macOS and Windows claim lody:// from Lody's bundle and installer
	for _, goos := range []string{"darwin", "windows"} {
		lodyGOOS = goos
		try(nil)
	}
}

func TestDesktopExec(t *testing.T) {
	sh := "/bin/sh"
	for _, c := range []struct {
		entry string
		want  []string
	}{
		{"[Desktop Entry]\nExec=/bin/sh -c 'x' %U\n", []string{sh, "-c", "'x'", "L"}},
		{"[Desktop Entry]\nExec=\"/bin/sh\" \"100%%\" %u %f %i\n", []string{sh, "100%", "L"}},
		{"[Desktop Action new]\nExec=/bin/false\n[Desktop Entry]\nExec=sh %U\n", []string{sh, "L"}},
		{"[Desktop Entry]\nName=Lody\n", nil},
		{"[Desktop Entry]\nExec=/no/such/lody %U\n", nil},
		{"[Desktop Entry]\nExec=no-such-lody-program %U\n", nil},
	} {
		got := desktopExec(c.entry, "L")
		if len(got) > 0 && got[0] != sh && filepath.Base(got[0]) == "sh" {
			got[0] = sh // sh found on PATH
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: %q, want %q", c.entry, got, c.want)
		}
	}
}
