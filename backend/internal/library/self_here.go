package library

// Repair built-in MCP server paths when the application has moved. A
// stored path whose program no longer exists is pointed at this build as
// the library is read. Existing alternate builds and unrelated commands
// remain the user's choice.

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/update"
)

// selfExe is this uni-switch by a path that stays put; a var for tests.
var selfExe = update.Program

// ownServer is whether s is uni-switch's own: `mcp …` run by a uni-switch binary,
// this computer's or another's.
func ownServer(s *Server) bool {
	return !s.Remote() && len(s.Args) > 0 && s.Args[0] == "mcp" && uniSwitchBinary(s.Command)
}

// uniSwitchBinary is whether cmd names a uni-switch program, by a path of any
// system's: uni-switch, uni-switch.exe, uni-switch-windows-amd64.exe, the AppImage.
func uniSwitchBinary(cmd string) bool {
	base := strings.ToLower(cmd[strings.LastIndexAny(cmd, `/\`)+1:])
	base = strings.TrimSuffix(base, ".exe")
	return base == uniSwitchCommand || strings.HasPrefix(base, uniSwitchCommand+"-")
}

// runsHere is whether cmd is a program by full path on this computer.
func runsHere(cmd string) bool {
	if !filepath.IsAbs(cmd) {
		return false
	}
	fi, err := os.Stat(cmd)
	return err == nil && !fi.IsDir()
}

// heal points uni-switch's own servers whose program isn't here at this uni-switch.
func (l *Library) heal() {
	exe := ""
	for _, s := range l.MCP {
		if s == nil || !ownServer(s) || runsHere(s.Command) {
			continue
		}
		if exe == "" {
			e, err := selfExe()
			if err != nil || e == "" {
				return
			}
			exe = e
		}
		s.Command = exe
	}
}
