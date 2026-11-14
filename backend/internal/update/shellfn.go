package update

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
)

// A command an agent is typed by, given to it by uni-switch: agy takes
// uni-switch's gateway, its key and the model only from how it is started
// (agent/agy.go), so uni-switch puts a function named agy in the profile of
// each shell the user has, which starts it through this uni-switch (bestony on
// Discord: no command to copy at each start). The function sits in a
// block of its own between two marked lines, which is all uni-switch ever
// writes or takes out of the profile; the rest of the file is the user's.
//
// The shells are those ReadCLI asks about: zsh (its .zshrc in ZDOTDIR when
// that is set), bash (the file a terminal's bash reads) and fish
// (config.fish) on macOS and Linux, PowerShell 7 and Windows PowerShell on
// Windows, each only where its execution policy runs a profile. A profile
// that isn't there yet is made only for the shell a terminal opens, and
// only a profile inside the home is written, so a test's temporary home
// keeps it from the real one.

// ShellBody is a function's text for one shell: "posix" (zsh, bash),
// "fish" or "powershell"; prog is this uni-switch, by a path that stays put.
type ShellBody func(kind, prog string) string

// ShellProfile is one shell's profile a function goes in.
type ShellProfile struct {
	Kind   string // posix, fish, powershell
	Path   string
	Create bool // made when it isn't there: the shell a terminal opens
}

// What the shell functions ask the system; tests set them.
var (
	fnProfiles = shellProfiles
	fnHome     = func() string {
		if runtime.GOOS == "windows" {
			return os.Getenv("USERPROFILE")
		}
		return os.Getenv("HOME")
	}
)

var fnMu sync.Mutex

func fnOpen(name string) string  { return "# >>> uni-switch " + name + " >>>" }
func fnClose(name string) string { return "# <<< uni-switch " + name + " <<<" }

// ShellFunction puts the function name, as body gives it for each shell,
// in the profile of every shell the user has, or brings one there up to
// date. It says the profiles that have it now. A profile that can't be
// read is left as it is: what it holds is unknown.
func ShellFunction(name string, body ShellBody) ([]string, error) {
	prog, err := cliExe()
	if err != nil {
		return nil, err
	}
	return ShellFunctionAt(homeProfiles(), name, func(kind string) string { return body(kind, prog) })
}

// DropShellFunction takes the function name out of every profile uni-switch
// put it in.
func DropShellFunction(name string) error { return DropShellFunctionAt(homeProfiles(), name) }

// ShellFunctionIn says the profiles that hold the function name.
func ShellFunctionIn(name string) []string { return ShellFunctionInAt(homeProfiles(), name) }

// homeProfiles are this machine's shells' profiles inside the user's home.
func homeProfiles() []ShellProfile {
	var out []ShellProfile
	for _, p := range fnProfiles() {
		if fnInHome(p.Path) {
			out = append(out, p)
		}
	}
	return out
}

// ShellFunctionAt is ShellFunction for the profiles ps of another home,
// which the caller names: a WSL distro's, opened from Windows, whose
// shells this uni-switch doesn't run in. body takes the shell's kind alone.
func ShellFunctionAt(ps []ShellProfile, name string, body func(kind string) string) ([]string, error) {
	fnMu.Lock()
	defer fnMu.Unlock()
	var in []string
	var errs []error
	for _, p := range ps {
		if !filepath.IsAbs(p.Path) {
			continue
		}
		ok, err := setFnBlock(p, name, strings.TrimRight(body(p.Kind), "\n"))
		if err != nil {
			errs = append(errs, err)
		}
		if ok {
			in = append(in, p.Path)
		}
	}
	return in, errors.Join(errs...)
}

// DropShellFunctionAt takes the function name out of the profiles ps.
func DropShellFunctionAt(ps []ShellProfile, name string) error {
	fnMu.Lock()
	defer fnMu.Unlock()
	var errs []error
	for _, p := range ps {
		if !filepath.IsAbs(p.Path) {
			continue
		}
		if _, err := setFnBlock(p, name, ""); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ShellFunctionInAt says which of the profiles ps hold the function name.
func ShellFunctionInAt(ps []ShellProfile, name string) []string {
	var in []string
	for _, p := range ps {
		b, err := os.ReadFile(p.Path)
		if err == nil && fnFind(lines(string(edit.TrimBOM(b))), name) >= 0 {
			in = append(in, p.Path)
		}
	}
	return in
}

// fnInHome says p is a file in the user's home.
func fnInHome(p string) bool {
	h := fnHome()
	if h == "" || !filepath.IsAbs(h) || !filepath.IsAbs(p) {
		return false
	}
	rel, err := filepath.Rel(h, p)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
}

// setFnBlock writes the function name's block in the profile p as body,
// or takes it out for ""; it says whether the block is there after.
func setFnBlock(p ShellProfile, name, body string) (bool, error) {
	b, err := os.ReadFile(p.Path)
	if errors.Is(err, fs.ErrNotExist) {
		if body == "" || !p.Create {
			return false, nil
		}
		b, err = nil, nil
	}
	if err != nil {
		return false, err
	}
	text := string(edit.TrimBOM(b))
	out := withFnBlock(text, name, body, p.Kind == "powershell")
	if out == text {
		return body != "", nil
	}
	if b == nil {
		if err := os.MkdirAll(filepath.Dir(p.Path), 0o755); err != nil {
			return false, err
		}
		// Windows PowerShell reads a profile without a byte order mark
		// in the system's code page, which a path of the user's name
		// outside ASCII would be garbled in
		if p.Kind == "powershell" {
			out = "\xef\xbb\xbf" + out
		}
	}
	if err := edit.WriteAtomic(p.Path, []byte(out)); err != nil {
		return false, err
	}
	return body != "", nil
}

func lines(text string) []string { return strings.Split(text, "\n") }

// fnFind is the line the function name's block opens on, -1 none.
func fnFind(ls []string, name string) int {
	for i, l := range ls {
		if strings.TrimRight(l, "\r") == fnOpen(name) {
			return i
		}
	}
	return -1
}

// withFnBlock is the profile text with the function name's block as body,
// in place of the one it has or after the rest; body "" takes the block
// out, and the blank line put before it. The file's own line endings are
// kept; a new file gets CRLF for PowerShell.
func withFnBlock(text, name, body string, crlf bool) string {
	nl := "\n"
	if strings.Contains(text, "\r\n") || text == "" && crlf {
		nl = "\r\n"
	}
	var block []string
	if body != "" {
		block = append(append([]string{fnOpen(name)}, lines(strings.ReplaceAll(body, "\r\n", "\n"))...), fnClose(name))
	}
	ls := lines(strings.ReplaceAll(text, "\r\n", "\n"))
	if i := fnFind(ls, name); i >= 0 {
		end := -1
		for j := i + 1; j < len(ls); j++ {
			if ls[j] == fnClose(name) {
				end = j
				break
			}
		}
		if end < 0 {
			// opened and never closed: someone cut it; the open line alone
			// is uni-switch's
			end = i
		}
		if body != "" {
			out := append(append(clone(ls[:i]), block...), ls[end+1:]...)
			return strings.Join(out, nl)
		}
		// out, with the blank line put before it
		from := i
		if i > 0 && ls[i-1] == "" {
			from = i - 1
		}
		return strings.Join(append(clone(ls[:from]), ls[end+1:]...), nl)
	}
	if body == "" {
		return text
	}
	var buf bytes.Buffer
	buf.WriteString(text)
	if text != "" {
		if !strings.HasSuffix(text, "\n") {
			buf.WriteString(nl)
		}
		buf.WriteString(nl)
	}
	buf.WriteString(strings.Join(block, nl) + nl)
	return buf.String()
}

func clone(s []string) []string { return append([]string(nil), s...) }

// TildeHome is p with the home as ~, for a message.
func TildeHome(p string) string { return tildeHome(p) }
