package sessions

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// imageLine is a dsh line carrying a pasted image of n base64 bytes, which
// zstd packs to about three quarters of its size.
func imageLine(r *rand.Rand, n int) string {
	const b64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	b := make([]byte, n)
	for i := range b {
		b[i] = b64[r.Intn(64)]
	}
	return `{"type":"user/message","data":{"content":[{"type":"image","data":"` + string(b) + `"}]}}`
}

// frameSize finds each frame's length from its headers, as readFrame
// reads it: frames with and without a checksum, with a long line, a
// skippable one (0, read by readFrame alone), and one cut short (0).
func TestFrameSizeIsWhatReadFrameReads(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	plain, _ := zstd.NewWriter(nil)
	crc, _ := zstd.NewWriter(nil, zstd.WithEncoderCRC(true))
	defer plain.Close()
	defer crc.Close()
	var file []byte
	var starts []int
	add := func(b []byte) { starts = append(starts, len(file)); file = append(file, b...) }
	add(crc.EncodeAll([]byte(imageLine(r, 3<<20)+"\n"), nil))
	add(plain.EncodeAll([]byte("{}\n\n{\"a\":1}\n"), nil))
	skippable := binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, 0x184D2A5E), 3)
	add(append(skippable, 1, 2, 3))
	add(plain.EncodeAll([]byte(imageLine(r, 2<<20)+"\n"), nil))
	add(crc.EncodeAll([]byte("{\"b\":2}\n"), nil))
	p := filepath.Join(t.TempDir(), "session.v3.jsonl.zstd")
	os.WriteFile(p, file, 0o600)
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rd := bufio.NewReader(f)
	for i, at := range starts {
		buf, n, err := readFrame(rd, nil, 0)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		size := frameSize(f, int64(at))
		if buf == nil && size != 0 || buf != nil && int64(size) != n {
			t.Fatalf("frame %d: frameSize %d, readFrame read %d", i, size, n)
		}
	}
	// a frame cut short has no size yet
	last := starts[3]
	if err := os.Truncate(p, int64(last+(starts[4]-last)/2)); err != nil {
		t.Fatal(err)
	}
	if size := frameSize(f, int64(last)); size != 0 {
		t.Fatalf("a frame cut short has size %d", size)
	}
}

// framesLines is every whole line of a file of zstd frames, decoded whole.
func framesLines(t *testing.T, p string) []string {
	t.Helper()
	raw, _ := os.ReadFile(p)
	d, _ := zstd.NewReader(nil)
	defer d.Close()
	dec, err := d.DecodeAll(raw, nil)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	var out []string
	for s := string(dec); strings.Contains(s, "\n"); {
		i := strings.IndexByte(s, '\n')
		out, s = append(out, s[:i]), s[i+1:]
	}
	return out
}

// scanFrames hands over the lines it handed over before frames were read
// into buffers of their own size: long lines in frames of their own, a
// line split across frames, empty lines, and a last frame still being
// written, read on once it is whole.
func TestScanFramesLongLinesUnchanged(t *testing.T) {
	r := rand.New(rand.NewSource(6))
	enc, _ := zstd.NewWriter(nil, zstd.WithEncoderCRC(true))
	defer enc.Close()
	long := imageLine(r, 3<<20)
	parts := [][]byte{
		enc.EncodeAll([]byte(`{"type":"session","id":"s"}`+"\n"), nil),
		enc.EncodeAll([]byte(imageLine(r, 5<<20)+"\n\n"+`{"seq":1}`+"\n"), nil),
		enc.EncodeAll([]byte(long[:len(long)/2]), nil), // a line split across frames
		enc.EncodeAll([]byte(long[len(long)/2:]+"\n"+`{"seq":2}`+"\n"), nil),
		enc.EncodeAll([]byte(imageLine(r, 1<<20)+"\n"), nil),
	}
	p := filepath.Join(t.TempDir(), "session.v3.jsonl.zstd")
	var whole []byte
	for _, b := range parts {
		whole = append(whole, b...)
	}
	os.WriteFile(p, whole, 0o600)
	want := framesLines(t, p)
	read := func(off, skip int64) (lines []string, next, nextSkip, end int64) {
		next, nextSkip, end, err := scanFrames(p, off, skip, func(b []byte) bool { lines = append(lines, string(b)); return true })
		if err != nil {
			t.Fatal(err)
		}
		return lines, next, nextSkip, end
	}
	got, _, _, end := read(0, 0)
	if end != int64(len(whole)) || !reflect.DeepEqual(got, want) {
		t.Fatalf("read %d lines to %d, want %d to %d", len(got), end, len(want), len(whole))
	}
	// the file as it was with the last frame half written, then whole
	cut := len(whole) - len(parts[4])/2
	os.WriteFile(p, whole[:cut], 0o600)
	first, next, nextSkip, end := read(0, 0)
	if end != int64(len(whole)-len(parts[4])) {
		t.Fatalf("a half-written last frame read to %d", end)
	}
	os.WriteFile(p, whole, 0o600)
	rest, _, _, _ := read(next, nextSkip)
	if got := append(first, rest...); !reflect.DeepEqual(got, want) {
		t.Fatalf("read on after a half-written frame: %d lines, want %d", len(got), len(want))
	}
}

// A frame holding a pasted image is read into one buffer of its own
// size. Grown a block at a time it allocated about 6 times the frame.
func TestScanFramesReadsALongFrameOnce(t *testing.T) {
	if raceOn {
		t.Skip("-race lets a pool's decoders go")
	}
	r := rand.New(rand.NewSource(7))
	enc, _ := zstd.NewWriter(nil, zstd.WithEncoderCRC(true))
	line := imageLine(r, 6<<20)
	frame := enc.EncodeAll([]byte(line+"\n"), nil)
	enc.Close()
	p := filepath.Join(t.TempDir(), "session.v3.jsonl.zstd")
	os.WriteFile(p, frame, 0o600)
	scan := func() {
		if _, _, _, err := scanFrames(p, 0, 0, func(b []byte) bool {
			if !bytes.Equal(b, []byte(line)) {
				t.Fatal("the line handed over is not the one written")
			}
			return true
		}); err != nil {
			t.Fatal(err)
		}
	}
	scan()
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	scan()
	runtime.ReadMemStats(&b)
	got := b.TotalAlloc - a.TotalAlloc
	t.Logf("a scan of a %d-byte frame of a %d-byte line allocated %d bytes", len(frame), len(line), got)
	// the frame once and the line once, decoded, and 2 MiB for the rest;
	// grown a block at a time the frame alone was ~25 MB of this
	if most := uint64(len(frame)+len(line)) + 2<<20; got > most {
		t.Fatalf("a scan of a %d-byte frame allocated %d bytes, want at most %d", len(frame), got, most)
	}
}
