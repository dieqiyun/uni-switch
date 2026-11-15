package gui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/filememo"
)

// The warm-up's reads look at a file once a second, not for each session
// or row that asks (#1665: settings.json ~300 times on Windows): a file
// another app changed meanwhile is read as it was, inside that second.
func TestWarmUpHoldsWhatItReads(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.json")
	old := time.Now().Add(-time.Hour)
	write := func(s string, at time.Time) {
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	read := func() string {
		b, err := filememo.Read("warm-hold-test", p, func(b []byte) (string, error) { return string(b), nil })
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	write("one", old)
	var got string
	warmUp(func() {
		read()
		write("two", old.Add(time.Minute))
		got = read()
	})
	if got != "one" {
		t.Fatalf("the warm-up looked at the file again for its second read: got %q", got)
	}
	// and nothing is held once it ends
	if got := read(); got != "two" {
		t.Fatalf("after the warm-up the file reads %q, not what is on the disk", got)
	}
}
