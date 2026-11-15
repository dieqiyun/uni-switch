package gateway

import (
	"net/http"
	"os"

	"github.com/dieqiyun/uni-switch/backend/internal/omarchy"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// siteOrigins are the pages that may read the Omarchy theme: io.uni-switch.desktop,
// which on an Omarchy machine draws itself in that machine's theme
// (site/public/omarchy.js) and asks the uni-switch running there which one it
// is; UNI_SWITCH_SITE_ORIGIN adds one (the site served locally to try it).
var siteOrigins = map[string]bool{}

func siteOrigin(o string) bool {
	return siteOrigins[o] || o != "" && o == os.Getenv("UNI_SWITCH_SITE_ORIGIN")
}

// onOmarchy is whether this uni-switch runs on Omarchy, asked once.
var onOmarchy = omarchy.Detect()

// omarchyTheme is Omarchy's current theme as uni-switch's own windows draw it
// (name, mode, stamp, CSS variables: palette, font, corners, border), for
// io.uni-switch.desktop's pages on the same machine. It answers this machine only,
// and the browser lets only io.uni-switch.desktop read it; a uni-switch elsewhere says
// 404.
func (s *Server) omarchyTheme(w http.ResponseWriter, r *http.Request) {
	if o := r.Header.Get("Origin"); siteOrigin(o) {
		w.Header().Set("Access-Control-Allow-Origin", o)
		w.Header().Set("Vary", "Origin")
		if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
			w.Header().Set("Access-Control-Allow-Private-Network", "true")
		}
	}
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !local(r) {
		writeError(w, provider.Chat, http.StatusForbidden, "uni-switch tells its Omarchy theme to this machine only")
		return
	}
	th, ok := omarchy.Theme{}, false
	if onOmarchy {
		th, ok = omarchy.Current()
	}
	if !ok {
		writeError(w, provider.Chat, http.StatusNotFound, "uni-switch isn't running on Omarchy")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, th)
}
