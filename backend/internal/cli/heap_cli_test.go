package cli

import (
	"bytes"
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// heapDir is a home of its own and a working folder in it, the one uni-switch
// heap writes its file to.
func heapDir(t *testing.T) (root, dir string) {
	t.Helper()
	root = helpHome(t)
	dir = filepath.Join(root, "work")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	return root, dir
}

// --help and its other forms print uni-switch heap's usage, ask nothing and
// write nothing, here or under the home; a flag it doesn't take, "-", or
// a second file are refused the same way, before the gateway is asked.
func TestHeapHelpAndBadArgsWriteNothing(t *testing.T) {
	root, _ := heapDir(t)
	asked := false
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { asked = true }))
	defer gw.Close()
	t.Setenv("UNI_SWITCH_ADDR", strings.TrimPrefix(gw.URL, "http://"))
	before := tree(t, root)
	for _, args := range [][]string{{"heap", "--help"}, {"heap", "-h"}, {"heap", "help"}, {"heap", "out.pb.gz", "--help"}} {
		out, err := runOut(t, args...)
		if err != nil || !strings.Contains(out, "usage: uni-switch heap [<file>]") {
			t.Errorf("uni-switch %s: %v %q", strings.Join(args, " "), err, out)
		}
	}
	for _, args := range [][]string{{"heap", "--out", "x"}, {"heap", "-o"}, {"heap", "-"}, {"heap", "a.pb.gz", "b.pb.gz"}, {"heap", " "}} {
		if _, err := runOut(t, args...); err == nil || !strings.Contains(err.Error(), "nothing was written") {
			t.Errorf("uni-switch %s: %v", strings.Join(args, " "), err)
		}
	}
	if asked {
		t.Error("the gateway was asked")
	}
	if got := tree(t, root); !reflect.DeepEqual(got, before) {
		t.Fatalf("written:\nbefore %v\nafter  %v", before, got)
	}
}

// Against a running gateway (its Handler, on loopback), uni-switch heap
// writes the gzipped profile into the working folder, prints its path and
// the memory numbers, and never writes over a file there.
func TestHeapWritesTheRunningGatewaysProfile(t *testing.T) {
	_, dir := heapDir(t)
	gw := httptest.NewServer(gateway.New().Handler())
	defer gw.Close()
	t.Setenv("UNI_SWITCH_ADDR", strings.TrimPrefix(gw.URL, "http://"))

	out, err := runOut(t, "heap", "mem.pb.gz")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "mem.pb.gz")
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := io.ReadAll(zr); err != nil || !bytes.Contains(raw, []byte("inuse_space")) {
		t.Fatalf("not a heap profile: %v", err)
	}
	if info, _ := os.Stat(file); info.Mode().Perm() != 0o600 && os.PathSeparator == '/' {
		t.Errorf("mode %v", info.Mode())
	}
	for _, want := range []string{"mem.pb.gz", "HeapAlloc", "HeapInuse", "HeapSys", "Sys ", "NumGC", "goroutines", "process"} {
		if !strings.Contains(out, want) {
			t.Errorf("printed no %s: %q", want, out)
		}
	}

	// the same name again: refused, the first file kept
	if _, err := runOut(t, "heap", "mem.pb.gz"); err == nil || !strings.Contains(err.Error(), "there already") {
		t.Fatalf("written over: %v", err)
	}
	if again, _ := os.ReadFile(file); !bytes.Equal(again, b) {
		t.Fatal("the first profile changed")
	}

	// no name: uni-switch-heap-<date>-<time>.pb.gz here
	if _, err := runOut(t, "heap"); err != nil {
		t.Fatal(err)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "uni-switch-heap-*.pb.gz")); len(m) != 1 {
		t.Fatalf("no default file: %v", m)
	}
}

// With no uni-switch answering, uni-switch heap says so and writes nothing.
func TestHeapWithNoGateway(t *testing.T) {
	_, dir := heapDir(t)
	// a port kept by the test that hangs up on every call: one freed
	// could be handed to anything next
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	t.Setenv("UNI_SWITCH_ADDR", ln.Addr().String())
	if _, err := runOut(t, "heap", "x.pb.gz"); err == nil || !strings.Contains(err.Error(), "is uni-switch running") {
		t.Fatalf("%v", err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("written: %v", ents)
	}
}
