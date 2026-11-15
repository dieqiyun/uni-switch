package gui

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// A uni-switch here that does not serve the gateway shows the routing of the one
// that does (#2606), and names it: the console reads the serving uni-switch's
// trace over loopback and plays it as its own, and its state carries how that
// uni-switch was started and its pid.
func TestConsoleShowsTheServingUniSwitchsRouting(t *testing.T) {
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
	other := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`{"name":"uni-switch","version":"0.1.1257","models":3,"window":true,"serves":"serve","pid":4321,"apis":[]}`))
		case "/v1/uni-switch/trace":
			w.Write([]byte(`{"object":"trace","seq":7,"pid":4321,"serves":"serve","routes":[{"id":1,"model":"ollama/gemma4:e4b","provider":"ollama","status":200}]}`))
		default:
			http.NotFound(w, r)
		}
	})}
	go other.Serve(ln)
	defer other.Close()
	served.Store(nil)

	mux := http.NewServeMux()
	traceRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/gateway/trace", nil))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var got struct {
		Mine bool `json:"mine"`
		By   *struct {
			Pid    int    `json:"pid"`
			Serves string `json:"serves"`
		} `json:"by"`
		Routes []struct {
			Model  string `json:"model"`
			Status int    `json:"status"`
		} `json:"routes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Mine || len(got.Routes) != 1 || got.Routes[0].Model != "ollama/gemma4:e4b" || got.Routes[0].Status != 200 {
		t.Fatalf("the serving uni-switch's routing was not shown: mine %v routes %+v", got.Mine, got.Routes)
	}
	// the trace names the uni-switch it came from, so the page's name for it
	// changes with the trace when another uni-switch takes the gateway over
	if got.By == nil || got.By.Pid != 4321 || got.By.Serves != "serve" {
		t.Errorf("the trace doesn't name the uni-switch it came from: %+v", got.By)
	}

	// the state names it, for the page's heading to say which one is routing
	g := providersState().Gateway
	if !g.Running || g.Mine || g.Serves != "serve" || g.Pid != 4321 {
		t.Errorf("state: running %v mine %v serves %q pid %d", g.Running, g.Mine, g.Serves, g.Pid)
	}
}

// One that doesn't tell its trace (older than the endpoint, or a refusal) is
// unknown, not empty: the page keeps the notice it had, and never plays
// another uni-switch's routing as its own.
func TestConsoleKeepsTheNoticeFromAnOlderGateway(t *testing.T) {
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
	other := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/" {
			w.Write([]byte(`{"name":"uni-switch","version":"0.1.550","models":3,"window":true,"apis":[]}`))
			return
		}
		http.NotFound(w, r)
	})}
	go other.Serve(ln)
	defer other.Close()
	served.Store(nil)

	mux := http.NewServeMux()
	traceRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/gateway/trace", nil))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var got struct {
		Mine   bool            `json:"mine"`
		Routes json.RawMessage `json:"routes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Mine {
		t.Fatalf("an older gateway's routing was played as this console's own: %s", w.Body)
	}
}
