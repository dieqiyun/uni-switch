package gui

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/dieqiyun/uni-switch/backend/internal/fonts"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// onFonts updates existing webviews even while the other one has focus.
var onFonts func()

func sameFont(a, b *fonts.Face) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func fontSettingsJS(s settings.Settings) string {
	b, _ := json.Marshal(map[string]any{"uiFont": s.UIFont, "codeFont": s.CodeFont})
	return "window.receiveFonts?.(" + string(b) + ");"
}

// The fonts installed on the computer uni-switch runs on, for its windows and
// for a browser tab (which keeps the families it draws itself, fonts.js).
// A browser tab is also offered common families: it may be on another
// computer, and a nogui build (Docker, uni-switch-cli) reads no fonts at all,
// where its rows were left out and only Text size flashed by (Norcleeh).
// A failed refresh leaves the last successful collection intact.
func fontRoutes(mux *http.ServeMux, web, off bool, discover func() ([]fonts.Family, error)) {
	var mu sync.Mutex
	var cached []fonts.Family
	mux.HandleFunc("GET /api/fonts", func(rw http.ResponseWriter, r *http.Request) {
		if off && !web {
			http.Error(rw, "installed fonts can't be read by this uni-switch", http.StatusNotFound)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if !off && (cached == nil || r.URL.Query().Get("refresh") == "1") {
			found, err := discover()
			switch {
			case err == nil && found == nil:
				cached = []fonts.Family{}
			case err == nil:
				cached = found
			case !web: // a tab still has the common families
				http.Error(rw, "could not read installed fonts", http.StatusServiceUnavailable)
				return
			}
		}
		if web {
			writeJSON(rw, withCommon(cached))
			return
		}
		writeJSON(rw, cached)
	})
}

// Families most computers have, by system and script; the tab keeps those
// it draws. Each is offered in its regular and bold weights.
var commonFamilies = []string{
	"Arial", "Avenir Next", "Calibri", "Cambria", "Cascadia Code", "Cascadia Mono", "Consolas", "Courier New",
	"DejaVu Sans", "DejaVu Sans Mono", "Fira Code", "Fira Sans", "Georgia", "Helvetica", "Helvetica Neue",
	"Hiragino Sans", "Hiragino Sans GB", "IBM Plex Mono", "IBM Plex Sans", "Inter", "JetBrains Mono",
	"Liberation Mono", "Liberation Sans", "Lucida Grande", "LXGW WenKai", "Malgun Gothic", "Maple Mono", "Menlo",
	"Microsoft JhengHei", "Microsoft YaHei", "Monaco", "Noto Sans", "Noto Sans CJK SC", "Noto Sans Mono",
	"Noto Serif", "Noto Serif CJK SC", "Palatino", "PingFang SC", "PingFang TC", "Roboto", "Roboto Mono",
	"Sarasa Gothic SC", "Segoe UI", "SF Mono", "SimHei", "SimSun", "Songti SC", "Source Code Pro",
	"Source Han Sans SC", "Tahoma", "Times New Roman", "Trebuchet MS", "Ubuntu", "Ubuntu Mono", "Verdana",
	"WenQuanYi Micro Hei", "Yu Gothic",
}

// withCommon adds the common families the list lacks, by name in any case,
// and keeps it in the order fonts.List gives (case-insensitive names).
func withCommon(found []fonts.Family) []fonts.Family {
	out := slices.Clone(found)
	have := map[string]bool{}
	for _, f := range found {
		have[strings.ToLower(f.Name)] = true
	}
	for _, name := range commonFamilies {
		if have[strings.ToLower(name)] {
			continue
		}
		face := fonts.Face{Family: name, Name: "Regular", Weight: 400, Style: "normal", Stretch: 100}
		bold := face
		bold.Name, bold.Weight = "Bold", 700
		out = append(out, fonts.Family{Name: name, Styles: []fonts.Face{face, bold}})
	}
	slices.SortStableFunc(out, func(a, b fonts.Family) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out
}
