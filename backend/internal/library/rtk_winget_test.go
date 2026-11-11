package library

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// wingetOn makes rtk upgraded as on Windows, with a fake winget whose
// winget list prints list and exits with code, and winget's records
// (where it put rtk-ai.rtk) given by records. It returns the file each
// run of winget list adds a line to.
func wingetOn(t *testing.T, h, list string, code int, records func() ([]string, bool)) string {
	t.Helper()
	tools := filepath.Join(h, "tools")
	ran := filepath.Join(h, "winget-ran")
	testenv.Program(t, filepath.Join(tools, "winget"), `#!/bin/sh
echo "$*" >> "`+ran+`"
case "$1" in
list) printf '`+list+`'; exit `+strconv.Itoa(code)+` ;;
esac
exit 2
`)
	t.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+tools)
	win, rec := rtkOnWindows, wingetRecords
	rtkOnWindows, wingetRecords = true, records
	forgetWingetOwned()
	t.Cleanup(func() {
		rtkOnWindows, wingetRecords = win, rec
		forgetWingetOwned()
	})
	return ran
}

func forgetWingetOwned() {
	rtkWingetOwned.Lock()
	clear(rtkWingetOwned.m)
	rtkWingetOwned.Unlock()
}

// winget list as Windows prints it with rtk-ai.rtk installed (the test
// box, winget v1.29.380), and without.
const (
	wingetListed   = `Name Id         Version Source\r\n-------------------------------\r\nrtk  rtk-ai.rtk v0.50.0 winget\r\n`
	wingetUnlisted = `No installed package found matching input criteria.\r\n`
)

func ranLines(t *testing.T, ran string) []string {
	t.Helper()
	b, err := os.ReadFile(ran)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// #1527 (whyIMBA): rtk installed with winget install --location
// "D:\codes\tools\rtk" — a folder with no "winget" in its name — had no
// Upgrade, though winget upgrade --id rtk-ai.rtk works on it. winget's
// record of where it put rtk-ai.rtk now says it is winget's.
func TestRTKWingetCustomLocation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fakes are shell scripts")
	}
	h := sandbox(t)
	folder := filepath.Join(h, "codes", "tools", "rtk")
	testenv.Program(t, filepath.Join(folder, "rtk"), strings.ReplaceAll(versionedRTK, "VERSION", filepath.Join(folder, "VERSION")))
	write(t, filepath.Join(folder, "VERSION"), "0.50.0")
	t.Setenv("PATH", folder)
	// the record, as Windows has it: InstallLocation D:\codes\tools\rtk,
	// in another case and with a trailing separator
	ran := wingetOn(t, h, wingetUnlisted, 20, func() ([]string, bool) {
		return []string{strings.ToUpper(folder) + string(filepath.Separator)}, true
	})
	v := ReadRTK()
	if !strings.HasPrefix(v.Upgrade, "winget upgrade --id rtk-ai.rtk --exact") {
		t.Fatalf("Upgrade %q; want winget upgrade", v.Upgrade)
	}
	// the record said; winget itself wasn't run
	if l := ranLines(t, ran); l != nil {
		t.Fatalf("winget ran: %q", l)
	}
}

// An rtk put somewhere by hand isn't taken for winget's, while winget has
// one of its own elsewhere.
func TestRTKWingetRecordElsewhere(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fakes are shell scripts")
	}
	h := sandbox(t)
	bin := filepath.Join(h, "codes", "rtk", "rtk")
	testenv.Program(t, bin, "#!/bin/sh\n")
	// a sibling folder whose name starts the same isn't inside it either
	ran := wingetOn(t, h, wingetListed, 0, func() ([]string, bool) {
		return []string{filepath.Join(h, "codes", "rt"), filepath.Join(h, "tools-rtk")}, true
	})
	if c := rtkUpgrader(bin); c != nil {
		t.Fatalf("upgrader %q; want none", c)
	}
	if l := ranLines(t, ran); l != nil {
		t.Fatalf("winget ran: %q", l)
	}
}

// With no record of winget's to read, winget list is asked, once in ten
// minutes for the same rtk.
func TestRTKWingetList(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fakes are shell scripts")
	}
	h := sandbox(t)
	bin := filepath.Join(h, "codes", "rtk", "rtk")
	testenv.Program(t, bin, "#!/bin/sh\n")
	none := func() ([]string, bool) { return nil, false }

	ran := wingetOn(t, h, wingetListed, 0, none)
	for range 3 {
		if c := rtkUpgrader(bin); len(c) == 0 || c[0] != "winget" || c[1] != "upgrade" {
			t.Fatalf("listed: upgrader %q; want winget upgrade", c)
		}
	}
	if l := ranLines(t, ran); len(l) != 1 || l[0] != "list --id rtk-ai.rtk --exact --accept-source-agreements --disable-interactivity" {
		t.Fatalf("winget ran %q; want winget list once", l)
	}
	// asked again once the ten minutes are up
	rtkWingetOwned.Lock()
	for k, o := range rtkWingetOwned.m {
		o.next = time.Now().Add(-time.Second)
		rtkWingetOwned.m[k] = o
	}
	rtkWingetOwned.Unlock()
	rtkUpgrader(bin)
	if l := ranLines(t, ran); len(l) != 2 {
		t.Fatalf("winget ran %q; want twice", l)
	}

	// winget has no rtk-ai.rtk: uni-switch can't tell, as before
	ran = wingetOn(t, h, wingetUnlisted, 20, none)
	if c := rtkUpgrader(bin); c != nil {
		t.Fatalf("unlisted: upgrader %q; want none", c)
	}
	// a record without a folder in it is no answer either
	wingetOn(t, h, wingetUnlisted, 20, func() ([]string, bool) { return nil, true })
	if c := rtkUpgrader(bin); c != nil {
		t.Fatalf("empty record, unlisted: upgrader %q; want none", c)
	}

	// a winget that doesn't answer in time isn't taken as yes
	tools := filepath.Join(h, "tools")
	testenv.Program(t, filepath.Join(tools, "winget"), "#!/bin/sh\nexec sleep 5\n")
	old := wingetListTimeout
	wingetListTimeout = 200 * time.Millisecond
	t.Cleanup(func() { wingetListTimeout = old })
	forgetWingetOwned()
	start := time.Now()
	if c := rtkUpgrader(bin); c != nil {
		t.Fatalf("hung winget: upgrader %q; want none", c)
	}
	if d := time.Since(start); d > 4*time.Second {
		t.Fatalf("waited %s for a hung winget", d)
	}
}

// winget's link in WinGet\Links to an rtk installed with --location is
// winget's by its folder, without asking it.
func TestRTKWingetLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fakes are shell scripts")
	}
	h := sandbox(t)
	real := filepath.Join(h, "tools-1527", "rtk", "rtk")
	testenv.Program(t, real, "#!/bin/sh\n")
	links := filepath.Join(h, "AppData", "Local", "Microsoft", "WinGet", "Links")
	os.MkdirAll(links, 0o755)
	if err := os.Symlink(real, filepath.Join(links, "rtk")); err != nil {
		t.Fatal(err)
	}
	ran := wingetOn(t, h, wingetUnlisted, 20, func() ([]string, bool) { return nil, false })
	if c := rtkUpgrader(filepath.Join(links, "rtk")); len(c) == 0 || c[0] != "winget" {
		t.Fatalf("through WinGet\\Links: upgrader %q; want winget upgrade", c)
	}
	if l := ranLines(t, ran); l != nil {
		t.Fatalf("winget ran: %q", l)
	}
}
