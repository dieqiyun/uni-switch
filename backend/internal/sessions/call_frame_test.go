package sessions

import (
	"bytes"
	"encoding/gob"
	"reflect"
	"testing"
	"time"
)

func encodedFrame(t *testing.T, v any) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := gob.NewEncoder(&b).Encode(v); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func freshFrame(t *testing.T, data []byte) callFrame {
	t.Helper()
	var f callFrame
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&f); err != nil {
		t.Fatal(err)
	}
	return f
}

// Frames decoded through one kept Decoder are the frames a Decoder of their
// own reads, frame after frame, a frame of other definitions among them; a
// cut or padded frame is an error, and the next frame still reads (#3493).
func TestDecodeFrameAsAFreshDecoder(t *testing.T) {
	var frames [][]byte
	for i := range 5 {
		f := callFrame{State: callDiskState{Path: "p", Size: int64(i), Mod: int64(i * 7)}, Strings: []string{"", "m", string(rune('a' + i))}}
		for j := range i {
			f.Patches = append(f.Patches, callPatch{Index: j, Text: [13]uint32{uint32(j)}, Began: time.Unix(int64(j), 0).UTC()})
		}
		frames = append(frames, encodedFrame(t, f))
	}
	// another writer's frame: other definitions, fields that land in callFrame
	type otherFrame struct{ Strings []string }
	frames = append(frames[:2], append([][]byte{encodedFrame(t, otherFrame{Strings: []string{"x"}})}, frames[2:]...)...)
	for round := range 2 {
		for i, data := range frames {
			var got callFrame
			if err := decodeFrame(data, &got); err != nil {
				t.Fatalf("round %d frame %d: %v", round, i, err)
			}
			if want := freshFrame(t, data); !reflect.DeepEqual(got, want) {
				t.Fatalf("round %d frame %d:\n got %+v\nwant %+v", round, i, got, want)
			}
		}
	}
	bad := map[string][]byte{
		"cut":    frames[3][:len(frames[3])-2],
		"padded": append(bytes.Clone(frames[3]), 1, 2),
		"empty":  nil,
		"types":  frames[3][:len(frames[3])/3],
	}
	for name, data := range bad {
		var f callFrame
		if decodeFrame(data, &f) == nil {
			t.Fatalf("a %s frame decoded", name)
		}
		var got callFrame
		if err := decodeFrame(frames[4], &got); err != nil || !reflect.DeepEqual(got, freshFrame(t, frames[4])) {
			t.Fatalf("after a %s frame: %v %+v", name, err, got)
		}
	}
}

// A frame of the definitions the last one had isn't compiled again: that
// was most of the reading of a start's ~15.6k shards (#3493).
func TestDecodeFrameKeepsDefinitions(t *testing.T) {
	if raceOn {
		t.Skip("-race lets a pool's decoders go")
	}
	data := encodedFrame(t, callFrame{Strings: []string{"", "m"}, Patches: []callPatch{{Index: 1}}})
	var f callFrame
	if err := decodeFrame(data, &f); err != nil {
		t.Fatal(err)
	}
	kept := testing.AllocsPerRun(50, func() {
		var f callFrame
		if err := decodeFrame(data, &f); err != nil {
			t.Fatal(err)
		}
	})
	fresh := testing.AllocsPerRun(50, func() {
		var f callFrame
		if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&f); err != nil {
			t.Fatal(err)
		}
	})
	if kept*4 > fresh {
		t.Fatalf("a kept Decoder made %v allocations a frame, a fresh one %v", kept, fresh)
	}
	t.Logf("allocations a frame: kept %v, fresh %v", kept, fresh)
}
