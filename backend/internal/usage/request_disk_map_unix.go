//go:build unix

package usage

import (
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

// mapDiskFile maps the file at path read-only. The file is only ever
// replaced by a rename (toDisk), never written in place, so what is
// mapped stays as it was read while the mapping lives.
// mapsFiles: the system maps a file, and archiveSpill holds a build's
// archives in one.
const mapsFiles = true

func mapDiskFile(path string) (*diskMapping, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() <= 0 || info.Size() != int64(int(info.Size())) {
		return nil, false
	}
	data, err := unix.Mmap(int(f.Fd()), 0, int(info.Size()), unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		return nil, false
	}
	m := &diskMapping{data: data}
	runtime.AddCleanup(m, func(data []byte) {
		unix.Munmap(data)
		if f := diskUnmapped.Load(); f != nil {
			(*f)()
		}
	}, data)
	return m, true
}

// read lets the system drop the pages a read of every entry touched: they
// are the file's, and read again from it when a chunk is decoded.
func (m *diskMapping) read() {
	unix.Madvise(m.data, unix.MADV_DONTNEED)
}
