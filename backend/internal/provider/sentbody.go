package provider

import (
	"errors"
	"io"
	"net/http"
	"sync/atomic"
)

// A request's body is held by the transport that sends it until its
// reply's body is closed: for a stream, the whole reply long. A body
// from bytes.NewReader, with the GetBody NewRequest makes for it, keeps
// every byte of the request alive that long; dozens of agents' long
// prompts streaming at once held hundreds of MB (#3383). SetBody's
// reader lets the bytes go once they are read, and its GetBody once
// LetGo says the request was answered.

// errLetGo is GetBody's error once the request was answered.
var errLetGo = errors.New("the request's body was let go once it was answered")

// heldBody is the bytes a request's GetBody reads again, until LetGo.
type heldBody struct{ b atomic.Pointer[[]byte] }

// sentBody is one read of a request's body, which drops what it has
// read once it reaches the end. Close leaves it as it is: the transport
// may close it while another of its goroutines reads it.
type sentBody struct {
	h *heldBody
	b []byte
}

func (s *sentBody) Read(p []byte) (int, error) {
	if len(s.b) == 0 {
		s.b = nil
		return 0, io.EOF
	}
	n := copy(p, s.b)
	if s.b = s.b[n:]; len(s.b) == 0 {
		s.b = nil
	}
	return n, nil
}

func (s *sentBody) Close() error { return nil }

// SetBody makes b req's body, as NewRequest would from a bytes.Reader,
// read again by GetBody for a retry or a redirect until LetGo.
func SetBody(req *http.Request, b []byte) {
	req.ContentLength = int64(len(b))
	if len(b) == 0 {
		req.Body, req.GetBody = http.NoBody, func() (io.ReadCloser, error) { return http.NoBody, nil }
		return
	}
	h := &heldBody{}
	h.b.Store(&b)
	req.Body = &sentBody{h: h, b: b}
	req.GetBody = func() (io.ReadCloser, error) {
		p := h.b.Load()
		if p == nil {
			return nil, errLetGo
		}
		return &sentBody{h: h, b: *p}, nil
	}
}

// LetGo lets go of the bytes SetBody gave req once it was answered and
// nothing will ask it again; a body SetBody didn't give is left as it is.
func LetGo(req *http.Request) {
	if req == nil {
		return
	}
	if s, ok := req.Body.(*sentBody); ok {
		s.h.b.Store(nil)
	}
}
