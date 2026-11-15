package usage

import "sync"

// chunkBuildKey is a session file as one query reads it: what prices and
// names a call (requestState's root and meta, hashed), and the file's
// path, size and time.
type chunkBuildKey struct {
	meta     [32]byte
	path     string
	size     int64
	modified int64
}

type chunkBuild struct {
	done chan struct{}
	c    *rowChunk // as made, never changed; nil when the make panicked
}

// chunkBuilds are the session files' chunks being made now, by the
// queries reading at once. At a cold start with the app open the
// warm-up's page and the GUI's heatmap each read every session file
// beside the other, and each held a chunk of each: twice the CPU, and
// 76 MB of chunks in the making at the peak (#2278).
var chunkBuilds = struct {
	sync.Mutex
	m map[chunkBuildKey]*chunkBuild
}{m: map[chunkBuildKey]*chunkBuild{}}

// buildJoined, in tests, is told each time a query waits for another's
// chunk.
var buildJoined func()

// sharedBuild is the chunk of k that build makes, or one another query is
// making of k now, once it is made. Each caller is handed a copy of its
// own, since a query writes its chunk's serial, its use and where its
// archive is; the rows and the archive are shared, and only read.
func sharedBuild(k chunkBuildKey, build func() *rowChunk) *rowChunk {
	chunkBuilds.Lock()
	if b := chunkBuilds.m[k]; b != nil {
		chunkBuilds.Unlock()
		if buildJoined != nil {
			buildJoined()
		}
		<-b.done
		if b.c == nil {
			return build()
		}
		c := *b.c
		return &c
	}
	b := &chunkBuild{done: make(chan struct{})}
	chunkBuilds.m[k] = b
	chunkBuilds.Unlock()
	defer func() {
		chunkBuilds.Lock()
		delete(chunkBuilds.m, k)
		chunkBuilds.Unlock()
		close(b.done)
	}()
	made := build()
	if made == nil {
		return nil
	}
	b.c = made
	c := *made
	return &c
}
