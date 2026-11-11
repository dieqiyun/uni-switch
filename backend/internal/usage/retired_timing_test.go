package usage

import (
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/s2"
)

func TestRetiredTimingSlotDoesNotShiftStoredUsage(t *testing.T) {
	c := &rowChunk{}
	c.add(Row{Record: Record{Time: time.Unix(1_790_000_000, 0), Model: "qa/model", Input: 17,
		UpBytes: 37, DownBytes: 41, ImageFilesSaved: 43, ImageFiles: 2}}, "", 0, false)
	archive, locations := c.encode(func([]byte) bool { return true })
	data, err := s2.Decode(nil, archive)
	if err != nil {
		t.Fatal(err)
	}
	// Older snapshots wrote their masking duration immediately before these
	// four counters. Give that retired slot a nonzero, multibyte value.
	if tail := data[len(data)-5:]; string(tail) != string([]byte{0, 37, 41, 43, 2}) {
		t.Fatalf("unexpected fixture suffix: %v", tail)
	}
	old := binary.AppendUvarint(data[:len(data)-5:len(data)-5], 10_000)
	old = append(old, 37, 41, 43, 2)
	packed := *c
	packed.Rows, packed.Archive, packed.Locations = nil, s2.Encode(nil, old), locations
	back := packed.unpack()
	if back == nil || len(back.Rows) != 1 {
		t.Fatal("old snapshot could not be read")
	}
	r := back.row(0)
	if r.Input != 17 || r.UpBytes != 37 || r.DownBytes != 41 || r.ImageFilesSaved != 43 || r.ImageFiles != 2 {
		t.Fatalf("old duration shifted counters: %+v", r)
	}
	out, _ := json.Marshal(r)
	if strings.Contains(string(out), "masking") {
		t.Fatalf("retired timing exposed: %s", out)
	}
}
