package sessions

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/filememo"
)

// desktopTitle is what a Desktop session record (local_<id>.json) says of
// its name and the CLI ids it ran under, in Desktop's own field names
// (Claude.app 2.7032's LocalSessionManager): its live handles are
// cliSessionId and unarchivedCliSessionId, its lineage preClearCliSessionId
// and priorCliSessionIds, and a record with no cliSessionId runs under its
// sessionId without "local_".
type desktopTitle struct {
	Session    string   `json:"sessionId"`
	CLI        string   `json:"cliSessionId"`
	Unarchived string   `json:"unarchivedCliSessionId"`
	PreClear   string   `json:"preClearCliSessionId"`
	Prior      []string `json:"priorCliSessionIds"`
	Title      string   `json:"title"`
}

var desktopTitles struct {
	sync.Mutex
	at    time.Time
	names map[string]string       // CLI id → name; "" when its owners disagree
	last  map[string]desktopTitle // metadata file → its last parse that read
	scans int                     // how many times the files were looked at, for tests
}

// desktopTitlesHeld is how long the names read stand before the files are
// looked at again: assemble asks once per Desktop session, and a list of a
// few hundred of them would otherwise stat every metadata file each time.
// A rename in Desktop shows within it. Tests set it to 0.
var desktopTitlesHeld = time.Second

// DesktopTitles reads Code and Cowork names from Desktop's local metadata,
// even when the CLI transcript is absent. Only IDs explicitly recorded there
// are named; prior CLI runs keep their own usage identity and share the name.
func DesktopTitles(ids []string) map[string]string {
	out := map[string]string{}
	if len(ids) == 0 {
		return out
	}
	desktopTitles.Lock()
	defer desktopTitles.Unlock()
	if desktopTitles.names == nil || time.Since(desktopTitles.at) >= desktopTitlesHeld {
		scanDesktopTitles()
	}
	for _, id := range ids {
		if name := desktopTitles.names[id]; name != "" {
			out[id] = name
		}
	}
	return out
}

// scanDesktopTitles rebuilds the names; the caller holds desktopTitles.
// A file that is there but can't be read or parsed (Desktop midway through
// writing it) is unknown, not unnamed: its last parse stands. A file gone
// takes its names with it.
func scanDesktopTitles() {
	desktopTitles.scans++
	names, ranks := map[string]string{}, map[string]int{}
	// A current ID takes precedence over a prior ID. Conflicting owners of
	// the same rank leave the name unknown instead of picking a profile.
	add := func(id, name string, rank int) {
		if id == "" || rank < ranks[id] {
			return
		}
		if rank > ranks[id] {
			ranks[id], names[id] = rank, name
		} else if names[id] != name {
			names[id] = ""
		}
	}
	last := map[string]desktopTitle{}
	for _, dir := range DesktopDataDirs() {
		for _, kind := range []string{"claude-code-sessions", "local-agent-mode-sessions"} {
			paths, _ := SessionGlob(filepath.Join(dir, kind, "*", "*", "local_*.json"))
			for _, path := range paths {
				info, err := os.Stat(path)
				if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
					continue
				}
				d, err := filememo.Read("session desktop title", path, func(b []byte) (desktopTitle, error) {
					var d desktopTitle
					err := json.Unmarshal(b, &d)
					d.Title = title(d.Title)
					return d, err
				})
				if err != nil {
					old, ok := desktopTitles.last[path]
					if !ok || errors.Is(err, fs.ErrNotExist) {
						continue
					}
					d = old
				}
				last[path] = d
				stem := strings.TrimPrefix(d.Session, "local_")
				if d.CLI == "" && stem != d.Session {
					add(stem, d.Title, 2)
				} else if stem != d.Session {
					add(stem, d.Title, 1)
				}
				add(d.CLI, d.Title, 2)
				add(d.Unarchived, d.Title, 2)
				add(d.PreClear, d.Title, 1)
				for _, id := range d.Prior {
					add(id, d.Title, 1)
				}
			}
		}
	}
	desktopTitles.names, desktopTitles.last, desktopTitles.at = names, last, time.Now()
}
