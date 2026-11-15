package provider

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A sign-in canceled while its token is being fetched adds no account
// (#2054), whichever way the code comes back: the browser's own callback
// or its address pasted. The token endpoint's reply is held until the
// cancel, then let go; before, the late token was kept as an account.
func TestCanceledSignInAddsNoAccount(t *testing.T) {
	for _, way := range []string{"browser callback", "pasted address"} {
		t.Run(way, func(t *testing.T) {
			home := claudeHome(t)
			hold := make(chan struct{})
			trades := fakeCodexTokens(t, hold)
			// before the fake's Close, which waits for the held trade
			let := sync.OnceFunc(func() { close(hold) })
			t.Cleanup(let)

			st, err := StartSignIn("codex")
			if err != nil {
				t.Fatal(err)
			}
			defer CancelSignIn(st.ID)
			good := callbackAddress(t, st, "cx", "")
			ended := make(chan error, 1)
			go func() {
				if way == "pasted address" {
					ended <- SubmitSignInCallback(st.ID, good)
					return
				}
				resp, err := (&http.Client{Timeout: time.Minute}).Get(strings.Replace(good, "localhost", "127.0.0.1", 1))
				if err == nil {
					_, err = io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
				ended <- err
			}()
			for i := 0; trades.Load() == 0; i++ {
				if i > 500 {
					t.Fatal("the code was never traded")
				}
				time.Sleep(10 * time.Millisecond)
			}
			CancelSignIn(st.ID)
			if cur, _ := SignInStatus(st.ID); cur.State != "canceled" {
				t.Fatalf("state after the cancel: %+v", cur)
			}
			let()
			err = <-ended
			if way == "pasted address" && err == nil {
				t.Error("the pasted address said the canceled sign-in finished")
			}
			if way == "browser callback" && err != nil {
				t.Fatal(err)
			}
			if cur, _ := SignInStatus(st.ID); cur.State != "canceled" {
				t.Errorf("state once the token came: %+v", cur)
			}
			if _, err := os.Stat(filepath.Join(home, ".codex", "auth.json")); err == nil {
				t.Error("the canceled sign-in wrote ~/.codex/auth.json")
			}
			for _, l := range Logins("codex") {
				if strings.EqualFold(l.User, "cx@example.com") {
					t.Errorf("the canceled sign-in added %s", l.User)
				}
			}
		})
	}
}

// heldReply lets a request through but holds its answer, read whole, until
// release: the answer came before the cancel, the account is kept after.
type heldReply struct {
	base    http.RoundTripper
	path    string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (h *heldReply) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := h.base.RoundTrip(r)
	if err != nil || r.URL.Path != h.path {
		return resp, err
	}
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(b))
	h.once.Do(func() { close(h.entered) })
	<-h.release
	return resp, nil
}

// syncBuffer is a log written from the sign-in's goroutine.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// A device-code sign-in (Copilot's) canceled once GitHub has given the
// token and said whose account it is, before uni-switch keeps it, adds no
// account (#2054).
func TestCanceledDeviceSignInAddsNoAccount(t *testing.T) {
	signIn(t)
	copilotFlowServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"access_token":"gho_late"}`)
	}, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"login":"late-user"}`)
	})
	h := &heldReply{base: http.DefaultTransport, path: "/copilot_internal/user",
		entered: make(chan struct{}), release: make(chan struct{})}
	let := sync.OnceFunc(func() { close(h.release) })
	t.Cleanup(let)
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = h
	t.Cleanup(func() { http.DefaultClient.Transport = old })
	var logged syncBuffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	st, err := StartSignInAt("copilot", "")
	if err != nil {
		t.Fatal(err)
	}
	defer CancelSignIn(st.ID)
	select {
	case <-h.entered:
	case <-time.After(30 * time.Second):
		t.Fatal("GitHub was never asked whose account it is")
	}
	CancelSignIn(st.ID)
	let()
	// the flow's goroutine ends either keeping the account or saying it
	// failed; wait for one of the two
	added := func() bool {
		for _, l := range Logins("copilot") {
			if strings.Contains(l.User, "late-user") {
				return true
			}
		}
		return false
	}
	for i := 0; !added() && !strings.Contains(logged.String(), "sign-in on github.com failed"); i++ {
		if i > 3000 {
			t.Fatalf("the sign-in never ended; log: %s", logged.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if added() {
		t.Error("the canceled sign-in added late-user")
	}
	if cur, _ := SignInStatus(st.ID); cur.State != "canceled" {
		t.Errorf("state: %+v", cur)
	}
}
