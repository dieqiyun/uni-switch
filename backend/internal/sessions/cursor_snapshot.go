package sessions

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// A Cursor store with a live -wal is read from a copy (see openCursorStore).
// Cursor's editor keeps every chat in one state.vscdb, often several GB, and
// a listing, the stats, the manage page, a transcript and a search over its
// chats each opened it again, so one visit to Sessions wrote the whole file
// to the temp folder five or more times (#1988). A copy of a large store is
// now kept while the store and its -wal are unchanged and shared by every
// reader, and is removed cursorSnapLinger after its last reader lets go.
var (
	cursorSnapLinger       = 30 * time.Second
	cursorSnapMin    int64 = 16 << 20
	// a uni-switch-cursor-* folder older than this is a copy a killed uni-switch
	// left behind; a shared copy in use is touched on every reuse
	cursorSnapStale = time.Hour
	cursorCopies    atomic.Int64
	// editor stores read in place beside a live Cursor (openCursorEditorStore)
	cursorInPlace atomic.Int64
	// false only in a test of the copy path
	cursorReadInPlace = true
)

type cursorSnap struct {
	path, dir, stamp string
	db               *sql.DB
	users            int
	last             time.Time
	gone             bool
}

var cursorSnaps struct {
	sync.Mutex
	m     map[string]*cursorSnap
	sweep sync.Once
}

// cursorStamp names a store's state by its and its -wal's size and time.
func cursorStamp(path string) (stamp string, size int64, ok bool) {
	for _, side := range []string{"", "-wal"} {
		fi, err := os.Stat(path + side)
		if err != nil {
			return "", 0, false
		}
		stamp += fmt.Sprintf("%d/%d;", fi.Size(), fi.ModTime().UnixNano())
		size += fi.Size()
	}
	return stamp, size, true
}

// sharedCursorStore hands out the kept copy of path while stamp still
// matches, and makes one otherwise.
func sharedCursorStore(path, stamp string, open func(string, string) (*sql.DB, error)) (*sql.DB, func(), error) {
	cursorSnaps.Lock()
	defer cursorSnaps.Unlock()
	if s := cursorSnaps.m[path]; s != nil {
		if s.stamp == stamp {
			s.users++
			now := time.Now()
			os.Chtimes(s.dir, now, now)
			return s.db, s.release(), nil
		}
		delete(cursorSnaps.m, path)
		s.gone = true
		if s.users == 0 {
			s.drop()
		}
	}
	db, dir, err := snapshotCursorStore(path, open)
	if err != nil {
		return nil, nil, err
	}
	s := &cursorSnap{path: path, dir: dir, stamp: stamp, db: db, users: 1}
	if cursorSnaps.m == nil {
		cursorSnaps.m = map[string]*cursorSnap{}
	}
	cursorSnaps.m[path] = s
	return db, s.release(), nil
}

func (s *cursorSnap) release() func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			cursorSnaps.Lock()
			defer cursorSnaps.Unlock()
			s.users--
			s.last = time.Now()
			if s.users > 0 {
				return
			}
			if s.gone {
				s.drop()
				return
			}
			time.AfterFunc(cursorSnapLinger, func() {
				cursorSnaps.Lock()
				defer cursorSnaps.Unlock()
				if s.gone || s.users > 0 || time.Since(s.last) < cursorSnapLinger {
					return
				}
				if cursorSnaps.m[s.path] == s {
					delete(cursorSnaps.m, s.path)
				}
				s.gone = true
				s.drop()
			})
		})
	}
}

func (s *cursorSnap) drop() {
	s.db.Close()
	os.RemoveAll(s.dir)
}

// snapshotCursorStore copies path and its -wal into a folder of uni-switch's
// own, cloning where the file system can so the copy takes no space.
func snapshotCursorStore(path string, open func(string, string) (*sql.DB, error)) (*sql.DB, string, error) {
	cursorSnaps.sweep.Do(sweepCursorSnaps)
	tmp, err := os.MkdirTemp("", "uni-switch-cursor-")
	if err != nil {
		return nil, "", err
	}
	copied := filepath.Join(tmp, "store.db")
	for _, side := range []string{"", "-wal"} {
		if cloneFile(path+side, copied+side) == nil {
			continue
		}
		os.Remove(copied + side)
		if err := copyFile(path+side, copied+side); err != nil {
			os.RemoveAll(tmp)
			return nil, "", err
		}
	}
	cursorCopies.Add(1)
	db, err := open(copied, "_pragma=busy_timeout(3000)")
	if err != nil {
		os.RemoveAll(tmp)
		return nil, "", err
	}
	return db, tmp, nil
}

// sweepCursorSnaps removes copies an earlier uni-switch was killed holding.
func sweepCursorSnaps() {
	old, _ := filepath.Glob(filepath.Join(os.TempDir(), "uni-switch-cursor-*"))
	for _, d := range old {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() && time.Since(fi.ModTime()) > cursorSnapStale {
			os.RemoveAll(d)
		}
	}
}
