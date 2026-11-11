package usage

import (
	"bytes"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

// What a write compares of an entry it wrote is the size, mtime and
// archive readDiskEntry reads of it, past any zones and actors, and an
// entry cut short anywhere is none. Each entry is made on the last one's
// bytes, as a write makes them.
func TestDiskEntryArchive(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skip(err)
	}
	var buf []byte
	for i, c := range []struct {
		zones  []*time.Location
		actors []string
	}{
		{nil, nil},
		{[]*time.Location{time.UTC, shanghai, time.FixedZone("X", 3600)}, []string{"cli", "exec", "a long actor name"}},
		{[]*time.Location{time.Local}, nil},
	} {
		chunk := &rowChunk{
			Source: sessions.CallSource{Path: "/s/rollout-" + string(rune('a'+i)) + ".jsonl", Key: "k", Agent: "codex",
				Size: int64(1000 + i), Modified: time.Unix(1790000000+int64(i), 123456789)},
			Count: 3, Latest: time.Unix(1790000100, 0), Locations: c.zones,
			Archive: bytes.Repeat([]byte{byte(i + 1)}, 100+i),
		}
		for _, a := range c.actors {
			chunk.codexActors.add(a)
		}
		b := diskEntry(buf, chunk)
		if b == nil {
			t.Fatalf("%d: no entry", i)
		}
		want, err := readDiskEntry(bytes.Clone(b))
		if err != nil {
			t.Fatalf("%d: %v", i, err)
		}
		size, modified, archive, ok := diskEntryArchive(b)
		if !ok || size != want.Source.Size || modified != want.Source.Modified.UnixNano() || !bytes.Equal(archive, want.Archive) {
			t.Fatalf("%d: %d %d %d bytes %v, want %d %d %d bytes", i, size, modified, len(archive), ok, want.Source.Size, want.Source.Modified.UnixNano(), len(want.Archive))
		}
		if !bytes.Equal(archive, chunk.Archive) {
			t.Fatalf("%d: archive is not the chunk's", i)
		}
		for n := range len(b) {
			if _, _, _, ok := diskEntryArchive(b[:n]); ok {
				t.Fatalf("%d: cut to %d of %d bytes still read", i, n, len(b))
			}
		}
		buf = b
	}
}

// An entry made on bytes big enough allocates nothing, nor does reading
// back what a write compares: each grown from nil was ~300 MB of a cold
// start's first write (#2278).
func TestDiskEntryAllocatesOnce(t *testing.T) {
	chunk := &rowChunk{
		Source: sessions.CallSource{Path: "/s/rollout.jsonl", Key: "k", Agent: "codex", Size: 1, Modified: time.Unix(1790000000, 0)},
		Count:  1, Latest: time.Unix(1790000000, 0), Locations: []*time.Location{time.UTC},
		Archive: bytes.Repeat([]byte{7}, 64<<10),
	}
	chunk.codexActors.add("cli")
	buf := diskEntry(nil, chunk)
	if n := testing.AllocsPerRun(20, func() { buf = diskEntry(buf, chunk) }); n != 0 {
		t.Errorf("an entry on the last one's bytes allocated %v times", n)
	}
	if n := testing.AllocsPerRun(20, func() { diskEntryArchive(buf) }); n != 0 {
		t.Errorf("reading an entry's archive allocated %v times", n)
	}
}

// A chunk without calls reads back without a Latest, as it was built: read
// as a call long ago, it was settled after a read and not after a build,
// and the heatmap's and effort's kept days were counted again (#2278).
func TestDiskEntryKeepsNoLatest(t *testing.T) {
	for _, latest := range []time.Time{{}, time.Unix(1790000100, 5)} {
		chunk := &rowChunk{
			Source: sessions.CallSource{Path: "/s/rollout.jsonl", Key: "k", Agent: "codex", Size: 1, Modified: time.Unix(1790000000, 0)},
			Latest: latest, Archive: []byte{1},
		}
		got, err := readDiskEntry(diskEntry(nil, chunk))
		if err != nil {
			t.Fatal(err)
		}
		if got.Latest.IsZero() != latest.IsZero() || !got.Latest.Equal(latest) {
			t.Errorf("Latest %v read back as %v", latest, got.Latest)
		}
	}
}
