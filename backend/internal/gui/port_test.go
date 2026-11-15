package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// freePort is a port nothing listens on now.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// answers says whether a uni-switch gateway answers on port p. Keep-alive is
// off: a moved gateway finishes the connections it has (Relisten), so a
// pooled one from the polls above would answer from the old port forever
// and say nothing about new connections to it.
func answers(p int) bool {
	c := &http.Client{Timeout: time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	res, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/", p))
	if err != nil {
		return false
	}
	res.Body.Close()
	return res.StatusCode == http.StatusOK
}

// Settings' gateway port (Magic_zero on Discord) moves the gateway this
// uni-switch serves: it answers on the new port and no longer on the old one,
// and uni-switch's URL, the CLI's and the agents', follows. A port another
// program has is refused with its reason, the gateway left where it was;
// one out of range or under UNI_SWITCH_ADDR is refused too.
func TestSetPortMovesTheGateway(t *testing.T) {
	sandboxHome(t)
	t.Setenv("UNI_SWITCH_ADDR", "")
	from := freePort(t)
	s := settings.Load()
	s.Port = from
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	if gateway.URL() != fmt.Sprintf("http://127.0.0.1:%d", from) {
		t.Fatalf("Settings' port isn't the gateway's: %s", gateway.URL())
	}
	ctx, cancel := context.WithCancel(context.Background())
	gw := gateway.New()
	done := make(chan struct{})
	go func() { defer close(done); gw.ListenAndServe(ctx) }()
	served.Store(gw)
	t.Cleanup(func() { served.Store(nil); cancel(); <-done })
	for deadline := time.Now().Add(5 * time.Second); !answers(from); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the gateway didn't start")
		}
	}

	handler := Handler(nil, nil)
	post := func(body string) (int, map[string]any) {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/settings/port", strings.NewReader(body)))
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}

	// a port another program listens on
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	tp := taken.Addr().(*net.TCPAddr).Port
	code, out := post(fmt.Sprintf(`{"port":%d}`, tp))
	if code != http.StatusBadRequest || !strings.Contains(fmt.Sprint(out["error"]), "in use") {
		t.Fatalf("a taken port: %d %v", code, out)
	}
	if settings.Load().Port != from || !answers(from) {
		t.Fatal("a taken port moved the gateway")
	}
	for _, bad := range []int{80, 70000} {
		if code, out := post(fmt.Sprintf(`{"port":%d}`, bad)); code != http.StatusBadRequest || !strings.Contains(fmt.Sprint(out["error"]), "1024 to 65535") {
			t.Fatalf("port %d: %d %v", bad, code, out)
		}
	}

	to := freePort(t)
	code, out = post(fmt.Sprintf(`{"port":%d}`, to))
	if code != http.StatusOK {
		t.Fatalf("moving: %d %v", code, out)
	}
	if p, _ := out["port"].(map[string]any); p == nil || p["port"] != float64(to) {
		t.Fatalf("the answer: %v", out)
	}
	if st, _ := out["settings"].(map[string]any); st["gateway"] != fmt.Sprintf("http://127.0.0.1:%d", to) || st["port"] != float64(to) {
		t.Fatalf("Settings say: %v", st)
	}
	if !answers(to) {
		t.Fatal("the gateway doesn't answer on the new port")
	}
	if answers(from) {
		t.Fatal("the gateway still answers on the old port")
	}
	if !gateway.Running() {
		t.Fatal("uni-switch doesn't find its own gateway on the new port")
	}

	// the Settings page's other choices, saved, leave the port as it is
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(`{"theme":"dark"}`)))
	if w.Code != http.StatusOK || settings.Load().Port != to {
		t.Fatalf("saving Settings took the port: %d %s, port %d", w.Code, w.Body, settings.Load().Port)
	}

	// UNI_SWITCH_ADDR comes first: the port is set there, not here
	t.Setenv("UNI_SWITCH_ADDR", "127.0.0.1:3426")
	if code, out := post(fmt.Sprintf(`{"port":%d}`, freePort(t))); code != http.StatusBadRequest || !strings.Contains(fmt.Sprint(out["error"]), "UNI_SWITCH_ADDR") {
		t.Fatalf("under UNI_SWITCH_ADDR: %d %v", code, out)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if !strings.Contains(w.Body.String(), `"addrEnv":"127.0.0.1:3426"`) {
		t.Fatalf("Settings don't say UNI_SWITCH_ADDR: %s", w.Body)
	}
}

// The port uni-switch was on taken by another program (Magic_zero's 3425), it
// serves nothing: setting another port starts the gateway there.
func TestSetPortServesWhenTheOldOneWasTaken(t *testing.T) {
	sandboxHome(t)
	t.Setenv("UNI_SWITCH_ADDR", "")
	other, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	srv := &http.Server{Handler: http.NotFoundHandler()}
	go srv.Serve(other)
	defer srv.Close()
	s := settings.Load()
	s.Port = other.Addr().(*net.TCPAddr).Port
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	served.Store(nil)
	t.Cleanup(func() { served.Store(nil) })
	if gateway.Running() {
		t.Fatal("another program taken for uni-switch")
	}
	to := freePort(t)
	if _, err := setPort(to); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); !answers(to); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the gateway didn't start on the new port")
		}
	}
}
