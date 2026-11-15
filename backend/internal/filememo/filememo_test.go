package filememo

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"
)

func TestRead(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	os.WriteFile(p, []byte("a"), 0o600)
	n := 0
	parse := func(b []byte) (string, error) { n++; return string(b), nil }
	for range 3 {
		if v, err := Read("t", p, parse); err != nil || v != "a" {
			t.Fatal(v, err)
		}
	}
	if n != 1 {
		t.Fatalf("parsed %d times", n)
	}
	os.WriteFile(p, []byte("bb"), 0o600)
	os.Chtimes(p, time.Now(), time.Now().Add(time.Second))
	if v, _ := Read("t", p, parse); v != "bb" || n != 2 {
		t.Fatalf("after a change: %q, parsed %d times", v, n)
	}
	os.Remove(p)
	if _, err := Read("t", p, parse); err == nil {
		t.Fatal("a file gone is an error")
	}
}

// two writes of one size inside one tick of the file system's clock (Linux
// stamps a file's time from a clock a few milliseconds coarse) leave the
// same time and size: the second is still read, not the first's parse
func TestReadSameSizeSameTick(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	parse := func(b []byte) (string, error) { return string(b), nil }
	for i := range 200 {
		a, b := fmt.Sprintf("a%03d,b", i), fmt.Sprintf("b%03d,a", i)
		os.WriteFile(p, []byte(a), 0o600)
		if v, _ := Read("tick", p, parse); v != a {
			t.Fatalf("read %q, want %q", v, a)
		}
		os.WriteFile(p, []byte(b), 0o600)
		if v, _ := Read("tick", p, parse); v != b {
			t.Fatalf("read %q after writing %q: the first write's parse", v, b)
		}
	}
}

// While a request holds what was read, a file found unchanged is not looked
// at again (the GUI's state looked a few thousand times, seconds on a slow
// disk); a write uni-switch makes (Forget), a second gone by or the release has
// it looked at, and a file just written is always read again.
func TestHold(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	os.WriteFile(p, []byte("a"), 0o600)
	old := time.Now().Add(-time.Minute)
	os.Chtimes(p, old, old)
	parse := func(b []byte) (string, error) { return string(b), nil }
	write := func(s string) {
		os.WriteFile(p, []byte(s), 0o600)
		at := old.Add(time.Duration(len(s)) * time.Second)
		os.Chtimes(p, at, at)
	}
	release := Hold()
	if v, _ := Read("hold", p, parse); v != "a" {
		t.Fatal(v)
	}
	write("bb")
	if v, _ := Read("hold", p, parse); v != "a" {
		t.Fatalf("looked at again while held: %q", v)
	}
	Forget()
	if v, _ := Read("hold", p, parse); v != "bb" {
		t.Fatalf("after a write uni-switch made: %q", v)
	}
	write("ccc")
	release()
	release()
	if v, _ := Read("hold", p, parse); v != "ccc" {
		t.Fatalf("released: %q", v)
	}
	if holds != 0 {
		t.Fatalf("holds left: %d", holds)
	}
	// a file written just now is read each time, held or not
	defer Hold()()
	os.WriteFile(p, []byte("d"), 0o600)
	Read("hold", p, parse)
	os.WriteFile(p, []byte("e"), 0o600)
	if v, _ := Read("hold", p, parse); v != "e" {
		t.Fatalf("a file written just now: %q", v)
	}
}

// A file not there is, while a request holds what was read, taken as
// still not there as a file found unchanged is: a provider with no list
// fetched was a look at the disk for each of its models, each request. A
// write uni-switch makes (Forget), another's (Look), or the hold's end has it
// looked at again; unheld, every read looks.
func TestHoldMissing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "none")
	parse := func(b []byte) (string, error) { return string(b), nil }
	looks := func() int {
		mu.Lock()
		defer mu.Unlock()
		return stats
	}
	release := Hold()
	before := looks()
	for range 100 {
		if _, err := Read("missing", p, parse); !os.IsNotExist(err) {
			t.Fatalf("a file not there: %v", err)
		}
	}
	if n := looks() - before; n != 1 {
		t.Fatalf("%d looks at a file not there, held", n)
	}
	os.WriteFile(p, []byte("a"), 0o600)
	if _, err := Read("missing", p, parse); !os.IsNotExist(err) {
		t.Fatalf("looked at again while held: %v", err)
	}
	Forget()
	if v, err := Read("missing", p, parse); v != "a" || err != nil {
		t.Fatalf("after a write uni-switch made: %q %v", v, err)
	}
	os.Remove(p)
	Read("missing", p, parse)
	os.WriteFile(p, []byte("b"), 0o600)
	Look(p)
	if v, err := Read("missing", p, parse); v != "b" || err != nil {
		t.Fatalf("after another's write: %q %v", v, err)
	}
	os.Remove(p)
	Read("missing", p, parse)
	release()
	os.WriteFile(p, []byte("c"), 0o600)
	if v, err := Read("missing", p, parse); v != "c" || err != nil {
		t.Fatalf("released: %q %v", v, err)
	}
	os.Remove(p)
	before = looks()
	for range 3 {
		Read("missing", p, parse)
	}
	if n := looks() - before; n != 3 {
		t.Fatalf("unheld, %d looks for 3 reads", n)
	}
}

// Written is closed by the next Forget, and a wait begun after it waits for
// the one after.
func TestWritten(t *testing.T) {
	w := Written()
	select {
	case <-w:
		t.Fatal("closed before any write")
	default:
	}
	Forget()
	select {
	case <-w:
	case <-time.After(5 * time.Second):
		t.Fatal("not closed by Forget")
	}
	select {
	case <-Written():
		t.Fatal("the next wait is closed already")
	default:
	}
}

// A test that saves inside a synctest bubble writes through Forget too,
// and neither the channel it closes nor the next one may be the bubble's:
// the next write from outside the bubble, another bubble's or none's,
// closing it is a fatal error that took internal/provider's tests down
// (#3632). Another process's write (wake) is held to the same, each in
// bubbles of its own, so neither cleans up after the other. Loops
// waiting outside are still told of a write made in a bubble, two of them
// on the one wait they share.
func TestWrittenAcrossBubbles(t *testing.T) {
	for _, write := range []struct {
		name string
		do   func()
	}{{"Forget", Forget}, {"wake", wake}} {
		w, also := Written(), Written() // two loops waiting
		for range 2 {
			synctest.Test(t, func(t *testing.T) { write.do() })
		}
		for _, c := range []<-chan struct{}{w, also} {
			select {
			case <-c:
			default:
				t.Fatalf("%s in a bubble left a wait taken outside it open", write.name)
			}
		}
		w = Written()
		write.do()
		select {
		case <-w:
		default:
			t.Fatalf("not closed by the %s after the bubbles'", write.name)
		}
	}
}
