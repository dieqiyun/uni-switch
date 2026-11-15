package codexrelay

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// The tests run in a home of their own, never the user's real files.
func TestMain(m *testing.M) { testenv.Main(m) }

// freeAddr is a loopback address nothing listens on, held until the
// relay binds it: a port freed here could be handed to anything next.
func freeAddr(t *testing.T) (string, net.Listener) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ln.Addr().String(), ln
}

// sleeper is a process of the test's own for the relay to watch, ended
// with the test.
func sleeper(t *testing.T) int {
	t.Helper()
	cmd := proc.Command("sleep", "600")
	if runtime.GOOS == "windows" {
		cmd = proc.Command("powershell", "-NoProfile", "-Command", "Start-Sleep 600")
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	return cmd.Process.Pid
}

func fast(t *testing.T) {
	t.Helper()
	wb, ww, wl := bindFor, watchEvery, leaveWait
	bindFor, watchEvery, leaveWait = 10*time.Second, 50*time.Millisecond, 5*time.Second
	t.Cleanup(func() { bindFor, watchEvery, leaveWait = wb, ww, wl })
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	end := time.Now().Add(20 * time.Second)
	for !ok() {
		if time.Now().After(end) {
			t.Fatalf("never: %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// The next uni-switch starting tells the relay to leave, and binds the port
// at once; the turn the relay is streaming then goes on to its end
// rather than being cut, and the relay ends after it.
func TestLeaveFreesPortAndFinishesStream(t *testing.T) {
	fast(t)
	addr, hold := freeAddr(t)
	pid := sleeper(t)
	release := make(chan struct{})
	streaming := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "first ")
		w.(http.Flusher).Flush()
		close(streaming)
		<-release
		io.WriteString(w, "last")
	})
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), addr, []int{pid}, h) }()
	// the relay waits for the port the quitting uni-switch still holds
	time.Sleep(300 * time.Millisecond)
	hold.Close()
	waitFor(t, "state written", func() bool { _, ok := readState(); return ok })

	type reply struct {
		body string
		err  error
	}
	got := make(chan reply, 1)
	go func() {
		res, err := http.Get("http://" + addr + "/backend-api/codex/responses")
		if err != nil {
			got <- reply{err: err}
			return
		}
		b, err := io.ReadAll(res.Body)
		res.Body.Close()
		got <- reply{string(b), err}
	}()
	<-streaming

	Leave()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("the port isn't free after Leave: %v", err)
	}
	ln.Close()
	select {
	case err := <-done:
		t.Fatalf("relay ended with a turn still streaming: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	r := <-got
	if r.err != nil || r.body != "first last" {
		t.Fatalf("the streaming turn was cut: %q %v", r.body, r.err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("relay didn't end after its last turn")
	}
	if _, ok := readState(); ok {
		t.Fatal("state left behind")
	}
}

// With every Codex process it was left for gone, the relay ends of
// itself and gives the port back.
func TestEndsWhenCodexGone(t *testing.T) {
	fast(t)
	addr, hold := freeAddr(t)
	hold.Close()
	pid := sleeper(t)
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), addr, []int{pid}, http.NotFoundHandler())
	}()
	waitFor(t, "state written", func() bool { _, ok := readState(); return ok })
	select {
	case <-done:
		t.Fatal("ended while Codex runs")
	case <-time.After(300 * time.Millisecond):
	}
	p, _ := os.FindProcess(pid)
	p.Kill()
	p.Wait()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("relay outlived the Codex it was left for")
	}
	if _, ok := readState(); ok {
		t.Fatal("state left behind")
	}
}

// Leave asks only with the relay's token: a request without it, or from
// a page, doesn't move the relay off the port. A relay that is gone
// leaves no state for the next start to ask.
func TestLeaveNeedsToken(t *testing.T) {
	fast(t)
	addr, hold := freeAddr(t)
	hold.Close()
	pid := sleeper(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, addr, []int{pid}, http.NotFoundHandler()) }()
	waitFor(t, "state written", func() bool { _, ok := readState(); return ok })
	st, _ := readState()
	for _, c := range []struct{ token, origin string }{{"", ""}, {"wrong", ""}, {st.Token, "null"}} {
		req, _ := http.NewRequest("POST", "http://"+addr+LeavePath, nil)
		if c.token != "" {
			req.Header.Set(TokenHeader, c.token)
		}
		if c.origin != "" {
			req.Header.Set("Origin", c.origin)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 403 {
			t.Fatalf("%+v: %d", c, res.StatusCode)
		}
	}
	if _, err := net.Listen("tcp", addr); err == nil {
		t.Fatal("the relay let go of the port without its token")
	}
	cancel()
	<-done
	// a state file whose relay is gone is removed, nothing asked
	if err := writeState(state{Pid: 999999, Addr: addr, Token: "x"}); err != nil {
		t.Fatal(err)
	}
	Leave()
	if _, err := os.Stat(statePath()); !os.IsNotExist(err) {
		t.Fatalf("dead relay's state kept: %v", err)
	}
	if !strings.HasPrefix(statePath(), settings.Dir()) {
		t.Fatal("state outside uni-switch's folder")
	}
}
