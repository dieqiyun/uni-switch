package provider

import (
	"io"
	"net/http"
	"runtime"
	"testing"
	"weak"
)

// A request's body is held by the transport until its reply's body is
// closed, a stream's whole length (#3383): SetBody's bytes can be let go
// once they are read and LetGo says the request was answered, while the
// request is still held, and until then GetBody reads them again whole.
func TestSetBodyLetsItsBytesGo(t *testing.T) {
	b := make([]byte, 4<<20)
	for i := range b {
		b[i] = byte('a' + i%26)
	}
	alive := weak.Make(&b[0])
	req, err := http.NewRequest(http.MethodPost, "http://example.invalid/v1", nil)
	if err != nil {
		t.Fatal(err)
	}
	SetBody(req, b)
	b = nil
	if req.ContentLength != 4<<20 {
		t.Fatalf("ContentLength %d", req.ContentLength)
	}
	if got, _ := io.ReadAll(req.Body); len(got) != 4<<20 || got[27] != 'b' {
		t.Fatalf("read %d bytes", len(got))
	}
	again, err := req.GetBody()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := io.ReadAll(again); len(got) != 4<<20 {
		t.Fatalf("GetBody read %d bytes, want the whole body again", len(got))
	}
	runtime.GC()
	if alive.Value() == nil {
		t.Fatal("the body was let go before LetGo, with GetBody still to read it")
	}
	LetGo(req)
	runtime.GC()
	if alive.Value() != nil {
		t.Error("the request holds its body's bytes after it was answered")
	}
	if _, err := req.GetBody(); err == nil {
		t.Error("GetBody read a body that was let go")
	}
	runtime.KeepAlive(req)
}

// An empty body is http.NoBody, as NewRequest makes it.
func TestSetBodyEmpty(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "http://example.invalid/v1", nil)
	SetBody(req, nil)
	if req.Body != http.NoBody || req.ContentLength != 0 {
		t.Errorf("body %T, length %d", req.Body, req.ContentLength)
	}
	LetGo(req)
}
