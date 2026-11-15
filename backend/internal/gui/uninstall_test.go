package gui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/uninstall"
)

// fakeUninstall puts fakes in for what Settings › Uninstall uni-switch does,
// and returns the order they ran in
func fakeUninstall(t *testing.T, agentErr string) *[]string {
	t.Helper()
	var ran []string
	sys, ag, data := uninstallSystem, uninstallAgents, uninstallData
	uninstallSystem = func(r *uninstall.Report) {
		ran = append(ran, "system")
		r.System = append(r.System, uninstall.Done{What: "background service"})
	}
	uninstallAgents = func(_ context.Context, r *uninstall.Report) {
		ran = append(ran, "agents")
		r.Agents = append(r.Agents, uninstall.Done{What: "Codex", Err: agentErr})
	}
	uninstallData = func() error { ran = append(ran, "data"); return nil }
	t.Cleanup(func() { uninstallSystem, uninstallAgents, uninstallData = sys, ag, data })
	return &ran
}

func postTo(mux http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", path, nil))
	return rec
}

// Uninstall from the app disconnects every agent after the service is
// gone, and says what it did; uni-switch's files go only on Finish, once the
// app quits, never with the agents.
func TestUninstallFromTheApp(t *testing.T) {
	ran := fakeUninstall(t, "")
	w := &quitCount{}
	mux := http.NewServeMux()
	uninstallRoutes(mux, w)
	rec := postTo(mux, "/api/uninstall")
	var j struct {
		Agents []uninstall.Done `json:"agents"`
		Failed bool             `json:"failed"`
		Says   []string         `json:"says"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &j) != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if j.Failed || len(j.Agents) != 1 || j.Agents[0].What != "Codex" || len(j.Says) != 2 {
		t.Fatalf("%s", rec.Body)
	}
	if got := *ran; len(got) != 2 || got[0] != "system" || got[1] != "agents" {
		t.Fatalf("ran %v", got)
	}
	if w.quits.Load() != 0 {
		t.Fatal("quit before Finish")
	}
	if rec := postTo(mux, "/api/uninstall/finish"); rec.Code != http.StatusNoContent {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	eventually(t, "Finish never quit", func() bool { return w.quits.Load() == 1 })
	if got := *ran; len(got) != 3 || got[2] != "data" {
		t.Fatalf("ran %v", got)
	}
}

// What couldn't be undone is said, for the page to show and not quit on.
func TestUninstallFromTheAppSaysWhatFailed(t *testing.T) {
	fakeUninstall(t, "config.toml: permission denied")
	mux := http.NewServeMux()
	uninstallRoutes(mux, &quitCount{})
	rec := postTo(mux, "/api/uninstall")
	var j struct {
		Failed bool     `json:"failed"`
		Says   []string `json:"says"`
	}
	if json.Unmarshal(rec.Body.Bytes(), &j) != nil || !j.Failed || j.Says[0] != "! disconnected Codex: config.toml: permission denied" {
		t.Fatalf("%s", rec.Body)
	}
}

// A Finish whose removal can't start doesn't quit: the files would stay
// with nothing left to say so.
func TestUninstallFinishThatCantStartStays(t *testing.T) {
	fakeUninstall(t, "")
	uninstallData = func() error { return errors.New("no exe") }
	w := &quitCount{}
	mux := http.NewServeMux()
	uninstallRoutes(mux, w)
	if rec := postTo(mux, "/api/uninstall/finish"); rec.Code < 400 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if w.quits.Load() != 0 {
		t.Fatal("quit")
	}
}

// uni-switch web never uninstalls: the page is on another computer, or a
// browser on this one that isn't the app.
func TestUninstallNeverFromABrowser(t *testing.T) {
	ran := fakeUninstall(t, "")
	mux := http.NewServeMux()
	uninstallRoutes(mux, webHost{func() { t.Error("quit") }})
	for _, p := range []string{"/api/uninstall", "/api/uninstall/finish"} {
		if rec := postTo(mux, p); rec.Code != http.StatusForbidden {
			t.Fatalf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
	if len(*ran) != 0 {
		t.Fatalf("ran %v", *ran)
	}
}
