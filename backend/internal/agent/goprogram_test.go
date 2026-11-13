package agent

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Telling a goose on PATH apart from a Go program reads it through a small
// buffer: buildinfo.ReadFile parsed a binary's whole symbol and string
// tables first, 200 MB allocated for Block's 260 MB goose on the first
// state the window asked for (procmem profile, #2515).
func TestGoProgramReadsNoSymbolTable(t *testing.T) {
	const strsize, nsyms = 32 << 20, 512 << 10
	bin := filepath.Join(t.TempDir(), "goose")
	// a Mach-O whose LC_SYMTAB names a 32 MB string table and 8 MB of
	// symbols, all zeros: a large program that isn't Go
	var hdr []byte
	le := binary.LittleEndian
	for _, v := range []uint32{0xfeedfacf, 0x0100000c, 0, 2, 1, 24, 0, 0, 0x2, 24, 4096, nsyms, 4096 + nsyms*16, strsize} {
		hdr = le.AppendUint32(hdr, v)
	}
	f, err := os.Create(bin)
	if err != nil {
		t.Fatal(err)
	}
	f.Write(hdr)
	f.Truncate(4096 + nsyms*16 + strsize)
	f.Close()

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	is := goProgram(bin)
	runtime.ReadMemStats(&after)
	if is {
		t.Fatal("a Mach-O without Go's build info taken for a Go program")
	}
	if got := after.TotalAlloc - before.TotalAlloc; got > 4<<20 {
		t.Fatalf("goProgram allocated %d MB for a 40 MB binary that isn't Go", got>>20)
	}
}

// The magic is found across two reads of the buffer, and a Go program is
// still one (TestGooseDetectSkipsGoGoose builds a real one).
func TestHasGoMagicAcrossReads(t *testing.T) {
	dir := t.TempDir()
	for _, at := range []int{0, 256<<10 - 5, 256<<10 - len(goBuildInfoMagic), 256 << 10, 600 << 10} {
		b := make([]byte, 700<<10)
		copy(b[at:], goBuildInfoMagic)
		p := filepath.Join(dir, "bin")
		if err := os.WriteFile(p, b, 0o755); err != nil {
			t.Fatal(err)
		}
		if !hasGoMagic(p) {
			t.Errorf("magic at %d not found", at)
		}
	}
	p := filepath.Join(dir, "none")
	os.WriteFile(p, make([]byte, 700<<10), 0o755)
	if hasGoMagic(p) {
		t.Error("magic found in zeros")
	}
	self, err := os.Executable()
	if err == nil && !goProgram(self) {
		t.Error("this test binary, a Go program, not taken for one")
	}
}
