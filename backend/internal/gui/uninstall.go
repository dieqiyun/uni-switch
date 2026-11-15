package gui

import (
	"errors"
	"net/http"

	"github.com/dieqiyun/uni-switch/backend/internal/uninstall"
)

// what Settings › Uninstall uni-switch asks of the system; the tests put
// fakes in
var (
	uninstallSystem = uninstall.System
	uninstallAgents = uninstall.Agents
	uninstallData   = uninstall.DataAfterExit
)

// uninstallRoutes: Settings › Uninstall uni-switch, the app's `uni-switch
// uninstall`. The app does it in two steps. POST /api/uninstall removes
// uni-switch's service, login item and shortcuts and disconnects every agent,
// putting back what each had, and answers with what it did, so the page
// can say what failed or that the Codex app needs a restart. POST
// /api/uninstall/finish then starts the removal of uni-switch's files once the
// app has quit (WebView2 holds its folder while the app runs), and quits.
// Never from a browser (uni-switch web): that would take uni-switch off the
// computer it serves from, not the one the page is on.
func uninstallRoutes(mux *http.ServeMux, w Windows) {
	web := isWeb(w)
	refuse := func(rw http.ResponseWriter) bool {
		if web {
			http.Error(rw, "uni-switch is uninstalled from the computer it runs on, with uni-switch uninstall there", http.StatusForbidden)
		}
		return web
	}
	mux.HandleFunc("POST /api/uninstall", func(rw http.ResponseWriter, r *http.Request) {
		if refuse(rw) {
			return
		}
		rep := &uninstall.Report{}
		// the service first: one still serving would put uni-switch back
		uninstallSystem(rep)
		uninstallAgents(r.Context(), rep)
		writeJSON(rw, struct {
			*uninstall.Report
			Failed bool     `json:"failed"`
			Says   []string `json:"says"`
		}{rep, rep.Failed(), rep.Says()})
	})
	mux.HandleFunc("POST /api/uninstall/finish", func(rw http.ResponseWriter, r *http.Request) {
		if refuse(rw) {
			return
		}
		if err := uninstallData(); err != nil {
			fail(rw, errors.New("uni-switch's files can't be removed after it quits: "+err.Error()))
			return
		}
		rw.WriteHeader(http.StatusNoContent)
		if f, ok := rw.(http.Flusher); ok {
			f.Flush()
		}
		go w.Quit()
	})
}
