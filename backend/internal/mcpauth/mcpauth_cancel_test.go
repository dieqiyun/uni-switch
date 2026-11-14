package mcpauth_test

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/mcpauth"
	"github.com/dieqiyun/uni-switch/backend/internal/mcpauth/mcpauthtest"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

type heldTokenResponse struct {
	base    http.RoundTripper
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (h *heldTokenResponse) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := h.base.RoundTrip(r)
	if err == nil && r.URL.Path == "/oauth/token" {
		h.once.Do(func() { close(h.entered) })
		<-h.release
	}
	return resp, err
}

// Cancel must still win after the browser claims its callback, while the
// token endpoint's successful reply is in flight.
func TestCanceledMCPCallbackDoesNotSave(t *testing.T) {
	for _, previous := range []bool{false, true} {
		name := "new sign-in"
		if previous {
			name = "existing sign-in"
		}
		t.Run(name, func(t *testing.T) {
			home(t)
			f := mcpauthtest.New(t)
			if previous {
				f.SignIn(t, "demo")
			}
			path := filepath.Join(settings.Dir(), "mcp-signins.json")
			before, err := os.ReadFile(path)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			h := &heldTokenResponse{
				base:    http.DefaultTransport,
				entered: make(chan struct{}),
				release: make(chan struct{}),
			}
			oldClient, oldChanged := mcpauth.Client, mcpauth.Changed
			mcpauth.Client = &http.Client{Transport: h}
			var changed atomic.Int32
			mcpauth.Changed = func(string) { changed.Add(1) }
			t.Cleanup(func() {
				mcpauth.Client, mcpauth.Changed = oldClient, oldChanged
			})
			st, err := mcpauth.Start(t.Context(), "demo", f.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer mcpauth.Cancel(st.ID)
			done := make(chan error, 1)
			go func() {
				resp, err := http.Get(st.URL)
				if err == nil {
					_, err = io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
				done <- err
			}()
			<-h.entered
			mcpauth.Cancel(st.ID)
			got, _ := mcpauth.Progress(st.ID)
			if got.State != "canceled" {
				t.Errorf("state after cancellation = %q", got.State)
			}
			close(h.release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Errorf("canceled callback saved credentials: previous sign-in = %v", previous)
			}
			if got := changed.Load(); got != 0 {
				t.Errorf("canceled callback redistributed credentials %d times", got)
			}
		})
	}
}
