package sessions

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
)

// PI-Desktop (vastsa/PI-Desktop) keeps its chats as its own jsonl, not
// Pi's, and their names only in its index database: pi.sqlite's sessions
// table (id, title, deleted_at; its crates/host-core/src/db/schema.rs). A
// chat it sends through uni-switch with X-Uni-Switch-Session is a gateway session
// with no file uni-switch reads, so it went by its id (#1967, dgmico); these
// are the names it goes by instead.

// PIDesktopDir is where PI-Desktop keeps its data: $PI_DESKTOP_DATA_DIR,
// else ~/.pi-desktop (a packaged install's, its 04-data-storage.md).
func PIDesktopDir() string {
	if d := appdir.Getenv("PI_DESKTOP_DATA_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pi-desktop")
}

var piDesktopTitles struct {
	sync.Mutex
	at    time.Time
	stamp string            // pi.sqlite's and its log's size and time when read
	names map[string]string // session id → its title
}

// piDesktopTitlesHeld is how long the names read stand before the
// database is looked at again. Tests set it to 0.
var piDesktopTitlesHeld = time.Second

// PIDesktopTitles are the titles PI-Desktop gave the chats ids name, those
// it has one for. pi.sqlite is only read: opened immutable, or from a copy
// when its write-ahead log holds what isn't in it yet, as Cursor's
// store.db is (openCursorStore). A database that can't be read leaves the
// names last read standing: unknown, not untitled.
func PIDesktopTitles(ids []string) map[string]string {
	if !Reads("pi") {
		return map[string]string{}
	}
	out := map[string]string{}
	if len(ids) == 0 {
		return out
	}
	piDesktopTitles.Lock()
	defer piDesktopTitles.Unlock()
	if piDesktopTitles.names == nil || time.Since(piDesktopTitles.at) >= piDesktopTitlesHeld {
		scanPIDesktopTitles()
	}
	for _, id := range ids {
		if name := piDesktopTitles.names[id]; name != "" {
			out[id] = name
		}
	}
	return out
}

// scanPIDesktopTitles reads the names again when pi.sqlite or its log
// changed; the caller holds piDesktopTitles.
func scanPIDesktopTitles() {
	piDesktopTitles.at = time.Now()
	path := filepath.Join(PIDesktopDir(), "pi.sqlite")
	main, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		piDesktopTitles.names, piDesktopTitles.stamp = map[string]string{}, ""
		return
	} else if err != nil {
		return
	}
	stamp := main.ModTime().String() + "|" + strconv.FormatInt(main.Size(), 10)
	if wal, err := os.Stat(path + "-wal"); err == nil {
		stamp += "|" + wal.ModTime().String() + "|" + strconv.FormatInt(wal.Size(), 10)
	}
	if piDesktopTitles.names != nil && stamp == piDesktopTitles.stamp {
		return
	}
	db, closeDB, err := openCursorStore(path)
	if err != nil {
		return
	}
	defer closeDB()
	rows, err := db.Query(`SELECT id, title FROM sessions WHERE deleted_at IS NULL AND title != ''`)
	if err != nil {
		return
	}
	defer rows.Close()
	names := map[string]string{}
	for rows.Next() {
		var id, name string
		if rows.Scan(&id, &name) == nil && id != "" {
			names[id] = title(name)
		}
	}
	if rows.Err() != nil {
		return
	}
	piDesktopTitles.names, piDesktopTitles.stamp = names, stamp
}

// isPIDesktop says whether a gateway session's agent may be PI-Desktop's:
// its own name, or Pi's, which it goes by unless given a User-Agent of
// its own (#1966).
func isPIDesktop(agent string) bool { return agent == "pi-desktop" || agent == "pi" }
