package gateway

import (
	"bytes"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// A zstd body is read through a decoder kept between requests: one made
// for each body allocated its window and block buffers every time, 1.7 GB
// of 9.8 GB in 10 minutes of three Codex sessions (procmem heap profile).
// What a read allocates past the decoded body itself stays small.
func TestZstdBodyDecoderKept(t *testing.T) {
	plain := []byte(`{"model":"fake/m1","input":"` + strings.Repeat("please look at file 12 and fix the bug ", 8000) + `"}`)
	enc, _ := zstd.NewWriter(nil)
	packed := enc.EncodeAll(plain, nil)
	enc.Close()
	read := func() {
		t.Helper()
		r := httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(packed))
		r.Header.Set("Content-Encoding", "zstd")
		body, status, err := readBoundedRequestBody(httptest.NewRecorder(), r, requestLimits{})
		if err != nil || status != 0 || !bytes.Equal(body, plain) {
			t.Fatalf("read %d bytes, status %d, %v", len(body), status, err)
		}
	}
	read() // the pool's first decoder
	if raceOn {
		t.Skip("-race: sync.Pool lets a quarter of what is put in it go")
	}
	const n = 20
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for range n {
		read()
	}
	runtime.ReadMemStats(&after)
	// io.ReadAll's growth to the body's size is ~2-3x it; a decoder made
	// for each body added its window and buffers on top
	per := (after.TotalAlloc - before.TotalAlloc) / n
	if per > uint64(4*len(plain)) {
		t.Fatalf("a %d KB body allocated %d KB to read", len(plain)>>10, per>>10)
	}
	t.Logf("a %d KB body: %d KB a read", len(plain)>>10, per>>10)
}
