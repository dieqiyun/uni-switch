package lastgood

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// zeros is what #1505's Windows crash left: logins.json at its full
// length, 10,783 bytes, every one 0x00.
func zeros(n int) []byte { return make([]byte, n) }

func setup(t *testing.T) string {
	t.Helper()
	Reset()
	t.Cleanup(Reset)
	return filepath.Join(t.TempDir(), "logins.json")
}

func TestZeroedFileReadFromItsBackup(t *testing.T) {
	p := setup(t)
	good := []byte(`[{"agent":"codex","user":"a@x.com"}]` + "\n")
	if err := os.WriteFile(p, good, 0o600); err != nil {
		t.Fatal(err)
	}
	// a write keeps the file there now as the last good generation
	if err := Keep(p, JSON, 0o600); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(Bak(p)); !bytes.Equal(b, good) {
		t.Fatalf(".bak = %q", b)
	}
	if err := os.WriteFile(p, zeros(10783), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := Read(p, JSON)
	if err != nil || !bytes.Equal(b, good) {
		t.Fatalf("a zeroed file read as %q, %v; want its backup", b, err)
	}
	n := Recovered()
	if len(n) != 1 || n[0].File != "logins.json" || n[0].Why != "10783 bytes, every one zero" || n[0].Bak.IsZero() {
		t.Fatalf("recovery noted as %+v", n)
	}
	// read again, it is noted once
	Read(p, JSON)
	if len(Recovered()) != 1 {
		t.Fatalf("noted %d times", len(Recovered()))
	}
}

// With no good backup, a zeroed file is unknown, never empty, and a file
// not there is still not there.
func TestZeroedFileWithoutBackupIsUnknown(t *testing.T) {
	p := setup(t)
	if _, err := Read(p, JSON); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a missing file read as %v", err)
	}
	os.WriteFile(p, zeros(29926), 0o600)
	if b, err := Read(p, JSON); !errors.Is(err, ErrUnreadable) || b != nil {
		t.Fatalf("a zeroed file without a backup read as %q, %v", b, err)
	}
	os.WriteFile(Bak(p), zeros(29926), 0o600) // the backup zeroed too
	if _, err := Read(p, JSON); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("a zeroed backup taken: %v", err)
	}
	if len(Recovered()) != 0 {
		t.Fatalf("noted %+v", Recovered())
	}
}

// Before a zeroed file is written over, it is copied aside and the good
// backup left as it is.
func TestKeepCopiesABadFileAside(t *testing.T) {
	p := setup(t)
	os.WriteFile(Bak(p), []byte(`["good"]`), 0o600)
	os.WriteFile(p, zeros(10783), 0o600)
	if err := Keep(p, JSON, 0o600); err != nil {
		t.Fatal(err)
	}
	kept, _ := filepath.Glob(p + ".bad-*")
	if len(kept) != 1 {
		t.Fatalf("kept %v", kept)
	}
	if b, _ := os.ReadFile(kept[0]); !bytes.Equal(b, zeros(10783)) {
		t.Fatalf("kept %d bytes", len(b))
	}
	if b, _ := os.ReadFile(Bak(p)); string(b) != `["good"]` {
		t.Fatalf("the good backup became %q", b)
	}
	// kept again (the write after failed): no second copy
	if err := Keep(p, JSON, 0o600); err != nil {
		t.Fatal(err)
	}
	if again, _ := filepath.Glob(p + ".bad-*"); len(again) != 1 {
		t.Fatalf("the same bad file kept twice: %v", again)
	}
}

// Two different bad files within one second are both kept; a name made
// from the clock is tried either side of a second's boundary.
func TestBadCopiesInOneSecondBothKept(t *testing.T) {
	p := setup(t)
	defer func() { now = time.Now }()
	at := time.Date(2026, 10, 10, 12, 0, 0, 999_000_000, time.Local)
	now = func() time.Time { return at }
	var names []string
	for i, b := range [][]byte{zeros(10), []byte(`{"half`), zeros(11)} {
		if i == 2 {
			at = at.Add(2 * time.Millisecond) // over the boundary
		}
		name, err := KeepBad(p, b)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(name); !bytes.Equal(got, b) {
			t.Fatalf("%s holds %q, want %q", name, got, b)
		}
		names = append(names, filepath.Base(name))
	}
	want := []string{"logins.json.bad-20261010-120000", "logins.json.bad-20261010-120000-2", "logins.json.bad-20261010-120001"}
	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Fatalf("kept as %v, want %v", names, want)
	}
}

// The same bad bytes kept a second earlier are not kept again: the copy
// is found whatever second its name was made in.
func TestSameBadFileKeptOnceAcrossASecond(t *testing.T) {
	p := filepath.Join(filepath.Dir(setup(t)), "[x]")
	if err := os.MkdirAll(p, 0o700); err != nil {
		t.Fatal(err)
	}
	p = filepath.Join(p, "logins.json")
	defer func() { now = time.Now }()
	at := time.Date(2026, 10, 10, 22, 56, 1, 999_000_000, time.Local)
	now = func() time.Time { return at }
	first, err := KeepBad(p, zeros(10783))
	if err != nil {
		t.Fatal(err)
	}
	at = at.Add(2 * time.Millisecond)
	again, err := KeepBad(p, zeros(10783))
	if err != nil {
		t.Fatal(err)
	}
	if again != first {
		t.Fatalf("kept again as %s, first as %s", filepath.Base(again), filepath.Base(first))
	}
	other, err := KeepBad(p, zeros(10784))
	if err != nil || other == first {
		t.Fatalf("a different bad file kept as %s (%v)", other, err)
	}
}

// A bad file that can't be copied aside stops the write.
func TestKeepFailsWhenTheBadFileCantBeKept(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a folder's mode doesn't stop a new file on Windows")
	}
	p := setup(t)
	os.WriteFile(p, zeros(64), 0o600)
	dir := filepath.Dir(p)
	os.Chmod(dir, 0o500)
	defer os.Chmod(dir, 0o700)
	if f, err := os.Create(filepath.Join(dir, "probe")); err == nil {
		f.Close()
		t.Skip("the filesystem does not enforce the read-only folder")
	}
	if err := Keep(p, JSON, 0o600); err == nil {
		t.Fatal("a bad file not kept, and the write let go on")
	}
}

func TestDescribe(t *testing.T) {
	for b, want := range map[string]string{
		string(zeros(3)): "3 bytes, every one zero",
		"":               "it is empty",
		"  \n":           "it is empty",
		`{"a":`:          "it doesn't parse",
	} {
		if got := describe([]byte(b)); got != want {
			t.Errorf("describe(%q) = %q, want %q", b, got, want)
		}
	}
	if JSON([]byte("\xef\xbb\xbf{}")) != true || JSON(zeros(5)) || JSON(nil) {
		t.Error("JSON misjudged")
	}
}

// KeepUnparsed keeps aside a file its reader can't read as its type (cut
// short, or valid JSON of another shape), leaves one that parses, an empty
// one and one not there alone, and keeps the same bad bytes once.
func TestKeepUnparsed(t *testing.T) {
	p := setup(t)
	parses := Into[map[string]string]
	bads := func() []string { m, _ := filepath.Glob(p + ".bad-*"); return m }
	if err := KeepUnparsed(p, parses); err != nil {
		t.Fatalf("a file not there: %v", err)
	}
	for _, ok := range []string{`{"a":"b"}`, "", "  \n", "null"} {
		os.WriteFile(p, []byte(ok), 0o600)
		if err := KeepUnparsed(p, parses); err != nil || len(bads()) != 0 {
			t.Fatalf("%q: err %v, kept %v", ok, err, bads())
		}
	}
	for i, bad := range []string{`[]`, `{"a":"b"`, `{"a":1}`} {
		os.WriteFile(p, []byte(bad), 0o600)
		for range 2 {
			if err := KeepUnparsed(p, parses); err != nil {
				t.Fatalf("%q: %v", bad, err)
			}
		}
		if got := bads(); len(got) != i+1 {
			t.Fatalf("%q: kept %v, want %d copies", bad, got, i+1)
		}
	}
}

// A file that doesn't parse and can't be copied aside stops the write.
func TestKeepUnparsedFailsWhenItCantBeKept(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a folder's mode doesn't stop a new file on Windows")
	}
	p := setup(t)
	os.WriteFile(p, []byte(`[]`), 0o600)
	dir := filepath.Dir(p)
	os.Chmod(dir, 0o500)
	defer os.Chmod(dir, 0o700)
	if f, err := os.Create(filepath.Join(dir, "probe")); err == nil {
		f.Close()
		t.Skip("the filesystem does not enforce the read-only folder")
	}
	if err := KeepUnparsed(p, Into[map[string]string]); err == nil {
		t.Fatal("a bad file not kept, and the write let go on")
	}
}
