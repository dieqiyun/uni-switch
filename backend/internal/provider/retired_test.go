package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func retiredLooks() int {
	retired.Lock()
	defer retired.Unlock()
	return retired.looks
}

// Every answered request asks Unretire (the gateway's servedCandidate):
// retired.json isn't looked at on disk for each, only once in retiredLook.
// What this process retires or brings back is told at once, another
// uni-switch's write within retiredLook, and a config path of its own (a new
// HOME) is read at once.
func TestRetiredLooksOnceInAWhile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	before := retiredLooks()
	for range 200 {
		Unretire("p", "m")
		if Retired("p", "m") {
			t.Fatal("retired out of nothing")
		}
	}
	if n := retiredLooks() - before; n > 2 {
		t.Fatalf("%d looks at retired.json for 200 requests", n)
	}
	Retire("p", "m")
	if !Retired("p", "m") {
		t.Fatal("this process's retirement isn't told at once")
	}
	Unretire("p", "m")
	if Retired("p", "m") {
		t.Fatal("this process's unretirement isn't told at once")
	}

	// another uni-switch's write
	defer func(d time.Duration) { retiredLook = d }(retiredLook)
	retiredLook = 20 * time.Millisecond
	b, _ := json.Marshal(map[string]time.Time{"p/other": time.Now()})
	if err := os.MkdirAll(filepath.Dir(retiredPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(retiredPath(), b, 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	os.Chtimes(retiredPath(), later, later)
	for deadline := time.Now().Add(10 * time.Second); !Retired("p", "other"); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("another uni-switch's write isn't seen")
		}
	}

	// a config of its own is read at once
	home2 := t.TempDir()
	t.Setenv("HOME", home2)
	t.Setenv("USERPROFILE", home2)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	retiredLook = time.Hour
	if Retired("p", "other") {
		t.Fatal("another config's retirement told")
	}
}

// BenchmarkUnretire is what an answered request's Unretire costs it.
func BenchmarkUnretire(b *testing.B) {
	home := b.TempDir()
	b.Setenv("HOME", home)
	b.Setenv("USERPROFILE", home)
	b.Setenv("XDG_CONFIG_HOME", b.TempDir())
	Retire("p", "x")
	for b.Loop() {
		Unretire("p", "m")
	}
}
