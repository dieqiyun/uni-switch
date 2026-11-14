package gateway

// GET /v1/uni-switch/goroutines: every goroutine of the process serving the
// gateway, with its stack and how long it has waited, for a report of
// uni-switch stuck at something while it goes on running. An app that kept
// the partner list it had for three days could only be read by killing
// it, which ends what was stuck (#3362); `uni-switch goroutines` asks the
// running process instead and writes the file the user sends.
//
// The dump is runtime/pprof's "goroutine" profile at debug=2, the form a
// crash prints: each goroutine's state, its wait ("[select, 4320
// minutes]"), its frames with source lines, and who started it. A frame's
// arguments are words in hex, never what they point at, so it holds no
// keys, prompts or messages; it names the source files uni-switch was built
// from and the program's path.

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// GoroutinesPath is where the gateway answers a goroutine dump.
const GoroutinesPath = "/v1/uni-switch/goroutines"

// GoroutineStatsHeader carries GoroutineStats, as JSON, beside the dump.
const GoroutineStatsHeader = "X-Uni-Switch-Goroutine-Stats"

// GoroutineStats says which process the dump is of.
type GoroutineStats struct {
	PID        int    `json:"pid"`
	Program    string `json:"program"`
	Version    string `json:"version"`
	Window     bool   `json:"window"` // the app's window or uni-switch web serves the gateway
	Uptime     string `json:"uptime"`
	Goroutines int    `json:"goroutines"`
}

// goroutinesRefused says why r may not have the goroutine dump, "" when it
// may: the same callers as the heap profile (heapRefused).
func goroutinesRefused(r *http.Request) string {
	return profileRefused(r, "goroutine dump", "uni-switch goroutines")
}

// dumpFirst answers the goroutine dump ahead of next, for every caller
// goroutinesRefused lets in; everything else, the dump's refusals too, goes
// to next as before. The dump is asked for when uni-switch hangs while it runs,
// and what the gateway's requests pass through first takes locks that a
// stuck request can hold: lanGuard's allow list reads settings.json under
// settings' file lock, holdCatalog takes provider.Hold's and filememo's,
// and each stats files under them. goroutinesRefused and goroutineDump
// read only the request, the environment and the runtime, so the dump
// answers while any of those is stuck, and names who holds it. A caller it
// lets in is one lanGuard and corsGuard let in too: loopback, which the
// allow list always passes, unproxied, no Origin or other browser mark, a
// loopback Host.
func (s *Server) dumpFirst(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == GoroutinesPath && goroutinesRefused(r) == "" {
			s.goroutineDump(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// goroutineDump answers GoroutinesPath: the dump as text, with
// GoroutineStats in GoroutineStatsHeader.
func (s *Server) goroutineDump(w http.ResponseWriter, r *http.Request) {
	if why := goroutinesRefused(r); why != "" {
		log.Printf("refused %s %s from %s: %s", r.Method, r.URL.Path, r.RemoteAddr, why)
		writeError(w, provider.Chat, http.StatusForbidden, why)
		return
	}
	goroutines := runtime.NumGoroutine()
	var buf bytes.Buffer
	if err := pprof.Lookup("goroutine").WriteTo(&buf, 2); err != nil {
		writeError(w, provider.Chat, http.StatusInternalServerError, "the goroutine dump couldn't be written: "+err.Error())
		return
	}
	exe, _ := os.Executable()
	stats, _ := json.Marshal(GoroutineStats{
		PID:        os.Getpid(),
		Program:    filepath.Base(exe),
		Version:    Version,
		Window:     Window,
		Uptime:     time.Since(processStarted).Round(time.Second).String(),
		Goroutines: goroutines,
	})
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="uni-switch-goroutines.txt"`)
	h.Set("Cache-Control", "no-store")
	h.Set(GoroutineStatsHeader, string(stats))
	w.Write(buf.Bytes())
}
