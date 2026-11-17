package gui

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/desktopupdate"
	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

func desktopUpdater(w Windows) *desktopupdate.Updater {
	switch host := w.(type) {
	case *uniSwitchHost:
		return host.updater
	case appWebHost:
		// The authenticated local browser page belongs to the same desktop
		// process; it must not fall back to the retired current-version stub.
		return desktopUpdater(host.app)
	}
	return nil
}

func uniSwitchUpdateAPI(w http.ResponseWriter, r *http.Request, u *desktopupdate.Updater) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet && r.URL.Path == "/api/update" {
		writeJSON(w, u.Snapshot())
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "unsupported update route", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Version   string `json:"version"`
		Task      string `json:"task"`
		AutoCheck *bool  `json:"autoCheck"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		http.Error(w, "invalid update request", http.StatusBadRequest)
		return
	}
	if decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid update request", http.StatusBadRequest)
		return
	}
	switch r.URL.Path {
	case "/api/update/preferences":
		if in.AutoCheck == nil {
			http.Error(w, "autoCheck is required", http.StatusBadRequest)
			return
		}
		if err := u.SetAutoCheck(*in.AutoCheck); err != nil {
			w.WriteHeader(http.StatusConflict)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
	case "/api/update/check":
		u.Check()
	case "/api/update/download":
		u.Download(in.Version)
	case "/api/update/cancel":
		u.Cancel()
	case "/api/update/install":
		if err := u.Install(in.Task); err != nil {
			w.WriteHeader(http.StatusConflict)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
	default:
		http.NotFound(w, r)
		return
	}
	writeJSON(w, u.Snapshot())
}

func (h *uniSwitchHost) installUpdate(path string) error {
	// No QA run may open a real installer or quit an installed application.
	if proc.Isolated() {
		return errors.New("installer launch is disabled in isolated QA")
	}
	switch runtime.GOOS {
	case "windows":
		exe := os.Getenv("UNI_SWITCH_DESKTOP_EXE")
		if !filepath.IsAbs(exe) {
			return errors.New("desktop installation location is unavailable")
		}
		// NSIS requires /D to be the final argument, without added quotes.
		// This also offers the current portable folder in the install wizard.
		if err := proc.Command(path, "/D="+filepath.Dir(exe)).Start(); err != nil {
			return err
		}
		time.AfterFunc(time.Second, h.quit)
		return nil
	case "darwin":
		return proc.Command("open", "--", path).Run()
	default:
		if filepath.Ext(path) == ".AppImage" {
			return h.OpenFolder(filepath.Dir(path))
		}
		return proc.Command("xdg-open", path).Run()
	}
}
