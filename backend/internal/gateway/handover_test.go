package gateway

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// serveOn starts s's ListenAndServe and waits for it to listen.
func listening(t *testing.T, s *Server) (stop context.CancelFunc, done <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan error, 1)
	go func() { ch <- s.ListenAndServe(ctx) }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		s.lnMu.Lock()
		ln := s.ln
		s.lnMu.Unlock()
		if ln != nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("the gateway never listened")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cancel, ch
}

// A restart to update hands the gateway's socket to the new uni-switch: a
// reply streaming through the old one when it does finishes there — the
// old one's end waits for it, rather than 2s — while new requests are
// answered by the new one, and none is refused on the way.
func TestHandedOverGatewayFinishesItsStream(t *testing.T) {
	fresh(t)
	release := make(chan struct{})
	var upstreamCalls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"stream":true`) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"quick"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"first \"}}]}\n\n")
		f.Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"last\"},\"finish_reason\":\"stop\"}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
		f.Flush()
	}))
	defer up.Close()
	defer close(release)
	if err := provider.Save(provider.Provider{ID: "plan", Name: "Plan", Key: "upstream", Chat: up.URL + "/v1", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	// the old one's end would wait this long without the handover: far
	// shorter than the stream is held below
	oldShort := shortDrain
	shortDrain = 10 * time.Millisecond
	t.Cleanup(func() { shortDrain = oldShort })

	t.Setenv("UNI_SWITCH_ADDR", "127.0.0.1:0")
	old := New()
	stopOld, oldDone := listening(t, old)
	defer stopOld()
	old.lnMu.Lock()
	addr := old.ln.Addr().String()
	old.lnMu.Unlock()
	t.Setenv("UNI_SWITCH_ADDR", addr) // as settings name it, for the new one

	// a stream under way through the old one
	req, _ := http.NewRequest("POST", "http://"+addr+"/v1/chat/completions", strings.NewReader(`{"model":"plan/m1","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	stream := bufio.NewReader(resp.Body)
	for {
		line, err := stream.ReadString('\n')
		if err != nil {
			t.Fatalf("the stream ended before its first chunk: %v", err)
		}
		if strings.Contains(line, "first") {
			break
		}
	}

	// the handover: the new one serves on the old one's socket
	f, err := old.ListenerFile()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.FileListener(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	acked := make(chan struct{})
	Inherit(ln, func() { close(acked) })
	next := New()
	stopNext, nextDone := listening(t, next)
	defer func() {
		stopNext()
		<-nextDone
	}()
	select {
	case <-acked:
	case <-time.After(10 * time.Second):
		t.Fatal("the new one never said it serves")
	}
	old.HandOff()
	stopOld()

	// new requests reach the new one, through the same port, while the
	// old one still streams
	for i := range 5 {
		r, err := http.Post("http://"+addr+"/v1/chat/completions", "application/json", strings.NewReader(chatReq))
		if err != nil {
			t.Fatalf("request %d during the handover: %v", i, err)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if r.StatusCode != 200 || !strings.Contains(string(b), "quick") {
			t.Fatalf("request %d during the handover: %d %s", i, r.StatusCode, b)
		}
	}
	if got := len(next.Recent()); got != 5 {
		t.Fatalf("the new one answered %d of the 5 requests after the handover", got)
	}

	// the old one's end waits for the stream (well past shortDrain)
	select {
	case err := <-oldDone:
		t.Fatalf("the old one ended with a stream in flight: %v", err)
	case <-time.After(20 * shortDrain):
	}
	if b := old.Busy(); b.Requests != 1 {
		t.Fatalf("the old one has %d requests in flight, want the stream", b.Requests)
	}
	release <- struct{}{}
	rest, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("the stream was cut: %v", err)
	}
	if !strings.Contains(string(rest), "last") || !strings.Contains(string(rest), "[DONE]") {
		t.Fatalf("the stream's end didn't arrive: %q", rest)
	}
	select {
	case err := <-oldDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the old one never ended once its stream had")
	}
	if n := upstreamCalls.Load(); n != 6 {
		t.Fatalf("upstream saw %d calls, want 6", n)
	}
}

// quickChat is a fresh HOME whose chat is answered at once ("quick").
func quickChat(t *testing.T) {
	t.Helper()
	fresh(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"quick"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`)
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "plan", Name: "Plan", Key: "upstream", Chat: up.URL + "/v1", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
}

// handOffSandbox is a gateway serving on a port of its own, its chat
// answered at once (quickChat), as the uni-switch to hand off; stop ends it
// and done says how it ended.
func handOffSandbox(t *testing.T) (old *Server, addr string, stop context.CancelFunc, done <-chan error) {
	t.Helper()
	quickChat(t)
	t.Setenv("UNI_SWITCH_ADDR", "127.0.0.1:0")
	old = New()
	stop, done = listening(t, old)
	t.Cleanup(stop)
	addr = old.Addr()
	t.Setenv("UNI_SWITCH_ADDR", addr) // as settings name it, for a new one
	return old, addr, stop, done
}

// chatOn sends the chat request on c, a connection of its own to addr.
func chatOn(t *testing.T, c net.Conn, addr string) {
	t.Helper()
	if _, err := fmt.Fprintf(c, "POST /v1/chat/completions HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", addr, len(chatReq), chatReq); err != nil {
		t.Fatal(err)
	}
}

// answerOn is the reply read on c, or why there was none.
func answerOn(c net.Conn) (string, error) {
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	r, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	return r.Status + " " + string(b), err
}

// A gateway handed off takes no connection from then on, its ctx not
// ended yet: one made then waits on the socket for the uni-switch it was
// handed to, which answers it. The old one took it before, until its end
// closed its listener, and closed it unanswered when it read the request
// after that end began (#3568).
func TestHandedOffGatewayTakesNoNewConnection(t *testing.T) {
	old, addr, stopOld, oldDone := handOffSandbox(t)
	defer func() {
		stopOld()
		<-oldDone
	}()
	f, err := old.ListenerFile()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.FileListener(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	old.HandOff()

	// made before the new one serves: only the old one could take it now
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	chatOn(t, c, addr)
	Inherit(ln, func() {})
	next := New()
	stopNext, nextDone := listening(t, next)
	defer func() {
		stopNext()
		<-nextDone
	}()
	if got, err := answerOn(c); err != nil || !strings.HasPrefix(got, "200 ") || !strings.Contains(got, "quick") {
		t.Fatalf("a request made once the gateway was handed off: %q %v", got, err)
	}
	if n := len(old.Recent()); n != 0 {
		t.Fatalf("the old one answered %d request(s) made once it had handed the gateway off", n)
	}
	if n := len(next.Recent()); n != 1 {
		t.Fatalf("the new one answered %d of the 1 request made once the gateway was handed off", n)
	}
}

// A connection the gateway took before it handed off, whose request comes
// only once its end has begun, is answered there: the end waits for every
// connection taken to send its first request before Shutdown, which closes
// one it reads after it began unanswered (#3568). So too for a `make dev`
// backend ending beside the one started after it (Handover).
func TestHandedOffGatewayAnswersAConnectionItTook(t *testing.T) {
	for _, dev := range []bool{false, true} {
		name := "handed off"
		if dev {
			name = "make dev"
		}
		t.Run(name, func(t *testing.T) {
			was := Handover
			Handover = dev
			defer func() { Handover = was }()
			old, addr, stopOld, oldDone := handOffSandbox(t)

			c, err := net.Dial("tcp", addr)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			// one made after it, answered: the old one took c first, as it
			// takes connections in the order they came
			one := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
			r, err := one.Post("http://"+addr+"/v1/chat/completions", "application/json", strings.NewReader(chatReq))
			if err != nil {
				t.Fatal(err)
			}
			r.Body.Close()
			if r.StatusCode != 200 {
				t.Fatalf("the request after it: %d", r.StatusCode)
			}

			if !dev {
				f, err := old.ListenerFile()
				if err != nil {
					t.Fatal(err)
				}
				ln, err := net.FileListener(f) // the new one's copy, keeping the socket open
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
				defer ln.Close()
				old.HandOff()
			}
			stopOld()
			// its agent sends the request late, once the end has begun:
			// well inside the 5s the end waits for it (unreadWait)
			time.Sleep(100 * time.Millisecond)
			chatOn(t, c, addr)
			if got, err := answerOn(c); err != nil || !strings.HasPrefix(got, "200 ") || !strings.Contains(got, "quick") {
				t.Fatalf("a request on a connection the old one took, sent once its end began: %q %v", got, err)
			}
			// its end follows the answer, not the 5s the wait gives up after
			// (unreadWait)
			select {
			case err := <-oldDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("the old one never ended once its connections were answered")
			}
			if n := len(old.Recent()); n != 2 {
				t.Fatalf("the old one answered %d of its 2 requests", n)
			}
		})
	}
}

// A kept-alive connection the gateway answered before it handed off,
// idle as its end begins, has the next request its agent sends answered
// there, the reply closing it: Shutdown closed it unanswered, and the
// agent's request just sent on it was cut ("connection reset", #3451).
// So too for a `make dev` backend ending beside the one started after it.
func TestHandedOffGatewayAnswersAKeptAliveConnection(t *testing.T) {
	for _, dev := range []bool{false, true} {
		name := "handed off"
		if dev {
			name = "make dev"
		}
		t.Run(name, func(t *testing.T) {
			was := Handover
			Handover = dev
			defer func() { Handover = was }()
			old, addr, stopOld, oldDone := handOffSandbox(t)

			c, err := net.Dial("tcp", addr)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			rd := bufio.NewReader(c)
			ask := func() (*http.Response, string, error) {
				chatOn(t, c, addr)
				c.SetReadDeadline(time.Now().Add(10 * time.Second))
				r, err := http.ReadResponse(rd, nil)
				if err != nil {
					return nil, "", err
				}
				defer r.Body.Close()
				b, err := io.ReadAll(r.Body)
				return r, string(b), err
			}
			// answered, and kept alive: idle once the end begins
			if r, b, err := ask(); err != nil || r.StatusCode != 200 || r.Close {
				t.Fatalf("the request before the handover: %v %q %v", r, b, err)
			}

			if !dev {
				f, err := old.ListenerFile()
				if err != nil {
					t.Fatal(err)
				}
				ln, err := net.FileListener(f) // the new one's copy, keeping the socket open
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
				defer ln.Close()
				old.HandOff()
			}
			stopOld()
			// its agent sends its next request once the end has begun:
			// well inside the 5s the end waits for it (unreadWait)
			time.Sleep(100 * time.Millisecond)
			r, b, err := ask()
			if err != nil || r.StatusCode != 200 || !strings.Contains(b, "quick") {
				t.Fatalf("a request on a kept-alive connection, sent once the end began: %q %v", b, err)
			}
			if !r.Close {
				t.Fatal("the reply kept the connection alive, for a request the old one's end would cut")
			}
			select {
			case err := <-oldDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("the old one never ended once its connections were answered")
			}
			if n := len(old.Recent()); n != 2 {
				t.Fatalf("the old one answered %d of its 2 requests", n)
			}
		})
	}
}

// lagListener's connections, once told to, sit a second after the write
// that ends the next chunked reply: as a busy machine can hold the
// goroutine that wrote a reply, its agent reading it, before net/http
// notes the connection idle.
type lagListener struct {
	net.Listener
	lag atomic.Bool
}

func (l *lagListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &lagConn{Conn: c, lag: &l.lag}, nil
}

type lagConn struct {
	net.Conn
	lag *atomic.Bool
}

func (c *lagConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if bytes.HasSuffix(b, []byte("0\r\n\r\n")) && c.lag.Swap(false) {
		time.Sleep(time.Second)
	}
	return n, err
}

// A kept-alive connection whose reply its agent has read, which net/http
// notes idle only once the end has begun (the goroutine that wrote it was
// held), has its next request answered there too: the end waited for new
// and idle connections only, so Shutdown closed this one as idle when
// net/http noted it, and the agent's next request was cut (ubuntu CI,
// 56602426d). So too for a `make dev` backend.
func TestHandedOffGatewayAnswersAConnectionStillAnswering(t *testing.T) {
	for _, dev := range []bool{false, true} {
		name := "handed off"
		if dev {
			name = "make dev"
		}
		t.Run(name, func(t *testing.T) {
			was := Handover
			Handover = dev
			defer func() { Handover = was }()
			quickChat(t)
			tcp, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			addr := tcp.Addr().String()
			t.Setenv("UNI_SWITCH_ADDR", addr)
			lagged := &lagListener{Listener: tcp}
			Inherit(lagged, func() {})
			old := New()
			stopOld, oldDone := listening(t, old)
			t.Cleanup(stopOld)

			c, err := net.Dial("tcp", addr)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			rd := bufio.NewReader(c)
			ask := func() (*http.Response, string, error) {
				chatOn(t, c, addr)
				c.SetReadDeadline(time.Now().Add(10 * time.Second))
				r, err := http.ReadResponse(rd, nil)
				if err != nil {
					return nil, "", err
				}
				defer r.Body.Close()
				b, err := io.ReadAll(r.Body)
				return r, string(b), err
			}
			lagged.lag.Store(true)
			if r, b, err := ask(); err != nil || r.StatusCode != 200 || r.Close {
				t.Fatalf("the request before the handover: %v %q %v", r, b, err)
			}
			// read, while the gateway's goroutine still sits in its write
			if !dev {
				f, err := tcp.(*net.TCPListener).File()
				if err != nil {
					t.Fatal(err)
				}
				ln, err := net.FileListener(f) // the new one's copy, keeping the socket open
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
				defer ln.Close()
				old.HandOff()
			}
			stopOld()
			// past the write's second, net/http has noted it idle: inside
			// the 5s the end waits (unreadWait)
			time.Sleep(1500 * time.Millisecond)
			r, b, err := ask()
			if err != nil || r.StatusCode != 200 || !strings.Contains(b, "quick") {
				t.Fatalf("a request on a connection still answering as the end began: %q %v", b, err)
			}
			if !r.Close {
				t.Fatal("the reply kept the connection alive, for a request the old one's end would cut")
			}
			select {
			case err := <-oldDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("the old one never ended once its connections were answered")
			}
			if n := len(old.Recent()); n != 2 {
				t.Fatalf("the old one answered %d of its 2 requests", n)
			}
		})
	}
}

// heldListener holds the next connection it takes, once told to, until
// let go: as a busy machine can hold the goroutine that took one, before
// net/http notes it.
type heldListener struct {
	net.Listener
	hold atomic.Bool
	took chan struct{} // closed once it holds one
	let  chan struct{} // closed to hand it on
}

func (l *heldListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil && l.hold.Swap(false) {
		close(l.took)
		<-l.let
	}
	return c, err
}

// A connection the gateway took just before its end began, which net/http
// notes only after that (the goroutine that took it was held), is answered
// there too: the end waits for Serve to return, by when every connection
// it took is noted. Shutdown marks the server shutting down before it waits
// for Serve, so a request on one noted after that is closed unanswered.
// So too for a `make dev` backend ending beside the one started after it.
func TestHandedOffGatewayAnswersAConnectionNotedLate(t *testing.T) {
	for _, dev := range []bool{false, true} {
		name := "handed off"
		if dev {
			name = "make dev"
		}
		t.Run(name, func(t *testing.T) {
			was := Handover
			Handover = dev
			defer func() { Handover = was }()
			quickChat(t)
			tcp, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			addr := tcp.Addr().String()
			t.Setenv("UNI_SWITCH_ADDR", addr)
			held := &heldListener{Listener: tcp, took: make(chan struct{}), let: make(chan struct{})}
			Inherit(held, func() {})
			old := New()
			stopOld, oldDone := listening(t, old)
			t.Cleanup(stopOld)
			letGo := sync.OnceFunc(func() { close(held.let) })
			t.Cleanup(letGo)

			held.hold.Store(true)
			c, err := net.Dial("tcp", addr)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			select {
			case <-held.took:
			case <-time.After(10 * time.Second):
				t.Fatal("the gateway never took the connection")
			}
			if !dev {
				f, err := tcp.(*net.TCPListener).File()
				if err != nil {
					t.Fatal(err)
				}
				ln, err := net.FileListener(f) // the new one's copy, keeping the socket open
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
				defer ln.Close()
				old.HandOff()
			}
			stopOld()
			// the end has begun, and would be in Shutdown by now, before
			// net/http notes the connection and reads its request
			time.Sleep(100 * time.Millisecond)
			chatOn(t, c, addr)
			letGo()
			if got, err := answerOn(c); err != nil || !strings.HasPrefix(got, "200 ") || !strings.Contains(got, "quick") {
				t.Fatalf("a request on a connection the old one took, noted once its end began: %q %v", got, err)
			}
			select {
			case err := <-oldDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("the old one never ended once its connection was answered")
			}
			if n := len(old.Recent()); n != 1 {
				t.Fatalf("the old one answered %d of its 1 request", n)
			}
		})
	}
}

// A socket handed over for another address is closed, and the gateway
// listens where settings say.
func TestInheritedForAnotherAddressIsLetGo(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	called := false
	Inherit(ln, func() { called = true })
	got, ack := takeInherited("127.0.0.1:1")
	if got != nil || ack != nil {
		t.Fatal("took a socket on another port")
	}
	if Inherited() {
		t.Fatal("the socket stayed waiting")
	}
	if _, err := ln.Accept(); err == nil {
		t.Fatal("the socket for another address wasn't closed")
	}
	if called {
		t.Fatal("acked a socket not served")
	}
}

func TestSameAddr(t *testing.T) {
	for _, tc := range []struct {
		ln   string
		addr string
		want bool
	}{
		{"127.0.0.1:3425", "127.0.0.1:3425", true},
		{"127.0.0.1:3425", "localhost:3425", true},
		{"127.0.0.1:3425", "127.0.0.1:3426", false},
		{"[::]:3425", "0.0.0.0:3425", true},
		{"0.0.0.0:3425", ":3425", true},
		{"0.0.0.0:3425", "127.0.0.1:3425", false},
		{"127.0.0.1:3425", "0.0.0.0:3425", false},
	} {
		a, err := net.ResolveTCPAddr("tcp", tc.ln)
		if err != nil {
			t.Fatal(err)
		}
		if got := sameAddr(a, tc.addr); got != tc.want {
			t.Errorf("sameAddr(%s, %s) = %v, want %v", tc.ln, tc.addr, got, tc.want)
		}
	}
}

// A Claude Code run of the uni-switch before this one, its turn under way when
// the gateway changed hands, calls its tools on the port this one now
// serves: the call goes on to that uni-switch's callbacks socket, through the
// network guard as its own run's would, and comes back answered. Once that
// uni-switch is gone, such a call is unknown here again, and guarded.
func TestToolCallOfTheUniSwitchBeforeGoesToIt(t *testing.T) {
	fresh(t)
	t.Setenv("UNI_SWITCH_ADDR", "0.0.0.0:3425")
	t.Setenv("UNI_SWITCH_TRUST_PROXY", "")
	newCaller(t, "Host") // gateway keys on
	sock := shortSock(t)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	var got atomic.Value
	before := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got.Store(r.Method + " " + r.URL.Path + " " + string(b))
		io.WriteString(w, `{"content":"answered there"}`)
	})}
	go before.Serve(ln)
	defer before.Close()
	ForwardCallbacks(sock)
	t.Cleanup(func() { ForwardCallbacks("") })

	h := localGuard(New().Handler())
	token := randomToken()
	call := func(remote string) (int, string) {
		r := httptest.NewRequest("POST", claudeMCPPath+token, strings.NewReader(`{"tool_call_id":"x","name":"read","arguments":{}}`))
		r.RemoteAddr = remote
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	// from this computer's own LAN address, as on a non-loopback UNI_SWITCH_ADDR
	// (#1546): no key, as for this one's own runs
	if c, b := call("192.168.1.9:5000"); c != 200 || !strings.Contains(b, "answered there") {
		t.Fatalf("the call wasn't passed on: %d %s", c, b)
	}
	if g, _ := got.Load().(string); !strings.HasPrefix(g, "POST "+claudeMCPPath+token+" ") || !strings.Contains(g, `"tool_call_id":"x"`) {
		t.Fatalf("the uni-switch before got %q", g)
	}
	// that uni-switch gone: the call is unknown here, and from the network it
	// needs a key again
	before.Close()
	os.Remove(sock)
	if c, b := call("127.0.0.1:5000"); c != http.StatusNotFound {
		t.Fatalf("with the uni-switch before gone: %d %s", c, b)
	}
	if forwarding() {
		t.Fatal("still passing calls on to a uni-switch gone")
	}
	if c, b := call("192.168.1.9:5000"); c != http.StatusUnauthorized {
		t.Fatalf("from the network, with the uni-switch before gone: %d %s", c, b)
	}
}

// The callbacks socket answers its own runs' tool calls and nothing else.
func TestCallbacksSocketServesOnlyToolCalls(t *testing.T) {
	fresh(t)
	s := New()
	sock := shortSock(t)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	go s.ServeCallbacks(ln)
	defer ln.Close()
	c := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", sock)
	}}}
	for _, p := range [][2]string{
		{"POST", claudeMCPPath + randomToken()},
		{"POST", "/v1/chat/completions"},
		{"GET", "/v1/models"},
		{"GET", claudeMCPPath + randomToken()},
	} {
		req, _ := http.NewRequest(p[0], "http://x"+p[1], strings.NewReader(chatReq))
		r, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode == 200 {
			t.Errorf("%s %s answered 200", p[0], p[1])
		}
	}
}

// A bridge turn counts while it runs, and its end is noted, for a restart
// to wait on: those are what a handover can't carry.
func TestBridgeTurnsCount(t *testing.T) {
	s := New()
	done := s.bridgeTurn()
	if b := s.Busy(); b.Bridge != 1 {
		t.Fatalf("Bridge = %d during a turn", b.Bridge)
	}
	before := time.Now()
	done()
	b := s.Busy()
	if b.Bridge != 0 || b.BridgeLast.Before(before) {
		t.Fatalf("after the turn: %+v", b)
	}
}

// shortSock is a unix socket path short enough for a Mac (104 bytes),
// which a test's TempDir may not be.
func shortSock(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "mho")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "cb")
}
