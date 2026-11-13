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

// --help and its other forms print uni-switch cpu's usage, ask nothing and
// write nothing, here or under the home; a flag it doesn't take, "-", a
// second file, or --seconds out of 5..120 are refused the same way,
// before the gateway is asked.
func TestCPUHelpAndBadArgsWriteNothing(t *testing.T) {
	root, _ := heapDir(t)
	asked := false
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { asked = true }))
	defer gw.Close()
	t.Setenv("UNI_SWITCH_ADDR", strings.TrimPrefix(gw.URL, "http://"))
	before := tree(t, root)
	for _, args := range [][]string{{"cpu", "--help"}, {"cpu", "-h"}, {"cpu", "help"}, {"cpu", "out.pb.gz", "--help"}, {"cpu", "--seconds", "10", "-h"}} {
		out, err := runOut(t, args...)
		if err != nil || !strings.Contains(out, "usage: uni-switch cpu [<file>] [--seconds N]") {
			t.Errorf("uni-switch %s: %v %q", strings.Join(args, " "), err, out)
		}
	}
	for _, args := range [][]string{
		{"cpu", "--out", "x"}, {"cpu", "-o"}, {"cpu", "-s", "10"}, {"cpu", "-"}, {"cpu", "a.pb.gz", "b.pb.gz"}, {"cpu", " "},
		{"cpu", "--seconds"}, {"cpu", "--seconds", "4"}, {"cpu", "--seconds", "121"}, {"cpu", "--seconds=0"}, {"cpu", "--seconds", "x"},
		{"cpu", "--seconds", "30s"}, {"cpu", "--seconds=-5"}, {"cpu", "--seconds", "10", "--seconds", "20"}, {"cpu", "x.pb.gz", "--seconds"},
	} {
		if _, err := runOut(t, args...); err == nil || !strings.Contains(err.Error(), "nothing was recorded") {
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

// --seconds N and --seconds=N are read, in any place among the file.
func TestCPUArgs(t *testing.T) {
	for _, c := range []struct {
		args []string
		file string
		secs int
	}{
		{nil, "", gateway.CPUSeconds},
		{[]string{"a.pb.gz"}, "a.pb.gz", 30},
		{[]string{"--seconds", "5"}, "", 5},
		{[]string{"--seconds=120", "a.pb.gz"}, "a.pb.gz", 120},
		{[]string{"a.pb.gz", "--seconds", "60"}, "a.pb.gz", 60},
	} {
		file, secs, err := cpuArgs(c.args)
		if err != nil || file != c.file || secs != c.secs {
			t.Errorf("%q: %q %d %v", c.args, file, secs, err)
		}
	}
}

// A file that is there already is refused before anything is recorded,
// and is kept as it was.
func TestCPUKeepsAFileThere(t *testing.T) {
	_, dir := heapDir(t)
	asked := false
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { asked = true }))
	defer gw.Close()
	t.Setenv("UNI_SWITCH_ADDR", strings.TrimPrefix(gw.URL, "http://"))
	file := filepath.Join(dir, "mine.pb.gz")
	if err := os.WriteFile(file, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runOut(t, "cpu", "mine.pb.gz"); err == nil || !strings.Contains(err.Error(), "there already") {
		t.Fatalf("written over: %v", err)
	}
	if b, _ := os.ReadFile(file); string(b) != "mine" {
		t.Fatalf("the file changed: %q", b)
	}
	if asked {
		t.Error("the gateway was asked")
	}
}

// Against a running gateway (its Handler, on loopback), uni-switch cpu says
// it records and for how long, then writes the gzipped profile into the
// working folder and prints its path, the CPU used and the goroutines.
// It records the 5 seconds asked for.
func TestCPUWritesTheRunningGatewaysProfile(t *testing.T) {
	_, dir := heapDir(t)
	gw := httptest.NewServer(gateway.New().Handler())
	defer gw.Close()
	t.Setenv("UNI_SWITCH_ADDR", strings.TrimPrefix(gw.URL, "http://"))

	out, err := runOut(t, "cpu", "--seconds", "5")
	if err != nil {
		t.Fatal(err)
	}
	m, _ := filepath.Glob(filepath.Join(dir, "uni-switch-cpu-*.pb.gz"))
	if len(m) != 1 {
		t.Fatalf("no default file: %v", m)
	}
	b, err := os.ReadFile(m[0])
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := io.ReadAll(zr); err != nil || !bytes.Contains(raw, []byte("samples")) || !bytes.Contains(raw, []byte("nanoseconds")) {
		t.Fatalf("not a CPU profile: %v", err)
	}
	if info, _ := os.Stat(m[0]); info.Mode().Perm() != 0o600 && os.PathSeparator == '/' {
		t.Errorf("mode %v", info.Mode())
	}
	for _, want := range []string{"Recording", "for 5s", "run Codex", filepath.Base(m[0]), "process", "CPU ", "of one core", "goroutines"} {
		if !strings.Contains(out, want) {
			t.Errorf("printed no %s: %q", want, out)
		}
	}
}

// With no uni-switch answering, uni-switch cpu says so at once and writes
// nothing.
func TestCPUWithNoGateway(t *testing.T) {
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
	out, err := runOut(t, "cpu", "x.pb.gz")
	if err == nil || !strings.Contains(err.Error(), "is uni-switch running") {
		t.Fatalf("%v", err)
	}
	if strings.Contains(out, "Recording") {
		t.Errorf("said it records: %q", out)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("written: %v", ents)
	}
}
