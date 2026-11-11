package library

// Reading the Library page compares skill folders whole: an agent's own
// skill against the same name in another agent (foundSkills), and each
// agent's copy against the library's skill (behind). Each compare read
// every byte of both folders on every read of the page, so a few dozen
// skills with node_modules or templates in them, given to several agents,
// read hundreds of megabytes each time the page opened; on Windows, with
// every file read scanned, that is minutes (0day404, #541). What a compare
// or a hash found is kept here against what a walk of the folders sees
// without opening a file: each entry's place, size and time. While those
// are the same, the answer is too. A folder that couldn't be walked or
// read whole is unknown: it is read again next time, never taken as
// unchanged.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	stdhash "hash"
	"io/fs"
	"path/filepath"
	"sync"
	"time"
)

// settleFor is how long ago the newest file of a folder must have been
// written for what was found of it to be kept: a file written again within
// the time a file system can tell apart could keep its time and size.
const settleFor = 3 * time.Second

// stamper sums what a walk sees of a folder's files and links without
// opening one: each one's place, type, size and time. The walk that reads
// a folder stamps it as it goes, before opening a file, so a file written
// while it is read has a stamp that says so next time.
type stamper struct {
	h      stdhash.Hash
	newest time.Time
}

func newStamper() *stamper { return &stamper{h: sha256.New()} }

func (s *stamper) add(rel string, fi fs.FileInfo) {
	fmt.Fprintf(s.h, "%s\x00%d\x00%d\x00%d\n", filepath.ToSlash(rel), fi.Mode().Type(), fi.Size(), fi.ModTime().UnixNano())
	if fi.ModTime().After(s.newest) {
		s.newest = fi.ModTime()
	}
}

// done is the stamp, and whether nothing stamped was written in the last
// settleFor.
func (s *stamper) done() (stamp string, settled bool) {
	return hex.EncodeToString(s.h.Sum(nil)), time.Since(s.newest) > settleFor
}

type hashMemo struct{ stamp, hash string }

type treeMemo struct {
	a, b string
	same bool
}

var memo = struct {
	sync.Mutex
	hashes map[string]hashMemo    // dir and what is left aside: the folder's hash
	trees  map[[2]string]treeMemo // two folders: whether they hold the same
}{hashes: map[string]hashMemo{}, trees: map[[2]string]treeMemo{}}

// rememberedHash is the hash of dir leaving lv aside: the hash found
// before, while a walk finds dir as it was then, else read. A folder
// hashed for the first time is walked once, as before the memo.
func rememberedHash(dir string, lv leave) string {
	key := fmt.Sprintf("%d\x00%s", lv, dir)
	memo.Lock()
	m, hit := memo.hashes[key]
	memo.Unlock()
	if hit {
		if _, stamp, _, ok := hashWalk(dir, lv, false); ok && stamp == m.stamp {
			return m.hash
		}
		memo.Lock()
		delete(memo.hashes, key)
		memo.Unlock()
	}
	h, stamp, settled, ok := hashWalk(dir, lv, true)
	if !ok {
		return ""
	}
	if settled {
		memo.Lock()
		memo.hashes[key] = hashMemo{stamp: stamp, hash: h}
		memo.Unlock()
	}
	return h
}

// rememberedSame is whether trees a and b hold the same, as found before
// while both are as they were then, else compare. Only an answer from
// reading both whole (sure) is kept.
func rememberedSame(a, b string, ta, tb tree, compare func() (same, sure bool)) bool {
	key := [2]string{a, b}
	memo.Lock()
	m, hit := memo.trees[key]
	memo.Unlock()
	if hit && m.a == ta.stamp && m.b == tb.stamp {
		return m.same
	}
	same, sure := compare()
	memo.Lock()
	if sure && ta.settled && tb.settled {
		memo.trees[key] = treeMemo{a: ta.stamp, b: tb.stamp, same: same}
	} else {
		delete(memo.trees, key)
	}
	memo.Unlock()
	return same
}
