package gui

import (
	"net"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// An older uni-switch keeping the gateway's port sends every agent's request
// its own way (#506: a Factory 403 worded as v0.1.550 words it, to people
// on v0.1.630). The Gateway page says which version serves and that it is
// older; a newer or the same one is just another uni-switch.
func TestGatewayServedByAnOlderUniSwitch(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(h, ".local", "share"))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("UNI_SWITCH_ADDR", ln.Addr().String())
	var answer string
	other := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(answer))
	})}
	go other.Serve(ln)
	defer other.Close()
	was := gateway.Version
	gateway.Version = "0.1.630"
	defer func() { gateway.Version = was }()
	served.Store(nil)

	for _, c := range []struct {
		answer, version string
		older, window   bool
	}{
		{`{"name":"uni-switch","version":"0.1.550","models":3,"window":true,"apis":[]}`, "0.1.550", true, true},
		{`{"name":"uni-switch","version":"0.1.630","models":3,"window":false}`, "0.1.630", false, false},
		{`{"name":"uni-switch","version":"0.1.640","models":3}`, "0.1.640", false, false},
		{`{"name":"uni-switch","models":3}`, "", false, false}, // one that doesn't say
	} {
		answer = c.answer
		g := providersState().Gateway
		if !g.Running || g.Mine || g.Version != c.version || g.Older != c.older || g.Window != c.window {
			t.Errorf("%s: running %v mine %v version %q older %v window %v", c.answer, g.Running, g.Mine, g.Version, g.Older, g.Window)
		}
	}
}
