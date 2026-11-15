//go:build !unix

package usage

// mapDiskFile maps nothing: on Windows a file mapped can't be renamed over,
// and toDisk replaces the file so. The file is read into memory instead.
const mapsFiles = false

func mapDiskFile(string) (*diskMapping, bool) { return nil, false }

func (m *diskMapping) read() {}
