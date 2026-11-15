package usage

import (
	"bufio"
	"os"
	"path/filepath"
)

// archiveSpill holds the archives a large build makes in a file of their
// own as each chunk is made, and maps the file once the build is done:
// kept on the heap until save wrote requests-v1.bin, a cold build's 31MB
// of archives (15.7k sessions) were most of what its peak held over a warm
// start's, and Go let the heap grow past them by as much again (#3493).
// Nil where the system maps no file: the archives would be read back into
// the heap.
type archiveSpill struct {
	f    *os.File
	w    *bufio.Writer
	off  int
	err  error
	held []spilled
}

// spilled is a chunk whose archive is in the spill, at off, n bytes long,
// and the copies of it the build hands on.
type spilled struct {
	copies []*rowChunk
	src    int
	off, n int
}

func newArchiveSpill() *archiveSpill {
	if !mapsFiles {
		return nil
	}
	return &archiveSpill{}
}

// put writes c's archive to the spill and lets go of it: true when it did.
// i is c's source, for done to make it again.
func (sp *archiveSpill) put(c *rowChunk, i int) bool {
	if sp == nil || sp.err != nil || len(c.Archive) == 0 || c.mapped != nil {
		return false
	}
	if sp.f == nil {
		dir := filepath.Dir(requestDiskPath())
		if sp.err = os.MkdirAll(dir, 0o700); sp.err != nil {
			return false
		}
		if sp.f, sp.err = os.CreateTemp(dir, "requests-*.spill"); sp.err != nil {
			return false
		}
		sp.w = bufio.NewWriterSize(sp.f, 1<<20)
	}
	if _, sp.err = sp.w.Write(c.Archive); sp.err != nil {
		return false
	}
	sp.held = append(sp.held, spilled{src: i, off: sp.off, n: len(c.Archive)})
	sp.off += len(c.Archive)
	c.Archive = nil
	return true
}

// hand records the copies of the chunk put last, which done gives its
// archive.
func (sp *archiveSpill) hand(copies ...*rowChunk) {
	sp.held[len(sp.held)-1].copies = copies
}

// done gives every chunk put its archive back, of the spill's pages. When
// the spill can't be written or mapped, each is made again with build from
// its source: none is left without one.
func (sp *archiveSpill) done(build func(int) *rowChunk) {
	if sp == nil || sp.f == nil {
		return
	}
	defer os.Remove(sp.f.Name())
	err := sp.err
	if err == nil {
		err = sp.w.Flush()
	}
	if closeErr := sp.f.Close(); err == nil {
		err = closeErr
	}
	var m *diskMapping
	if err == nil {
		if mm, ok := mapDiskFile(sp.f.Name()); ok && len(mm.data) == sp.off {
			m = mm
			m.spill = true
		}
	}
	for _, s := range sp.held {
		var archive []byte
		if m != nil {
			archive = m.data[s.off : s.off+s.n : s.off+s.n]
		} else {
			archive = build(s.src).Archive
		}
		for _, c := range s.copies {
			c.Archive, c.mapped = archive, m
		}
	}
}

// pagesRank orders where a chunk's archive is: the heap, a build's spill,
// requests-v1.bin. A copy of the same chunk takes the later.
func (c *rowChunk) pagesRank() int {
	switch {
	case c.mapped == nil:
		return 0
	case c.mapped.spill:
		return 1
	}
	return 2
}
