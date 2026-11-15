package gateway

// GET /v1/uni-switch/cpu: a Go CPU profile of the process serving the gateway,
// recorded for some seconds while the user goes on using uni-switch, for a
// report of uni-switch using too much CPU (@LeiCheng42 on X: the most CPU of
// anything while Codex runs). A macOS `sample` or Activity Monitor names
// none of Go's frames, so this is the one way to see where the Go side
// spends it; `uni-switch cpu` asks for it and writes the file the user sends.

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// CPUPath is where the gateway answers a CPU profile.
const CPUPath = "/v1/uni-switch/cpu"

// CPUStatsHeader carries CPUStats, as JSON, beside the profile.
const CPUStatsHeader = "X-Uni-Switch-CPU-Stats"

// The seconds a CPU profile records: CPUSeconds unless the caller asks
// for others, from CPUMinSeconds to CPUMaxSeconds.
const (
	CPUSeconds    = 30
	CPUMinSeconds = 5
	CPUMaxSeconds = 120
)

// CPUStats is what the process used while the profile recorded. CPU is
// the process's own user and system time over those seconds, as the
// system counts it (all its threads, Go's or not), and CPUTotal since it
// started; both are 0 with CPUKnown false where the system doesn't say.
type CPUStats struct {
	PID        int     `json:"pid"`
	Program    string  `json:"program"`
	Version    string  `json:"version"`
	Window     bool    `json:"window"` // the app's window or uni-switch web serves the gateway
	Uptime     string  `json:"uptime"`
	Seconds    float64 `json:"seconds"` // how long the profile recorded
	CPUKnown   bool    `json:"cpuKnown"`
	CPU        float64 `json:"cpu"`      // seconds of CPU used while recording
	CPUTotal   float64 `json:"cpuTotal"` // seconds of CPU used since the process started
	Goroutines int     `json:"goroutines"`
	NumCPU     int     `json:"numCPU"`
	GOMAXPROCS int     `json:"gomaxprocs"`
}

// cpuRecording is true while a CPU profile records: runtime/pprof keeps
// one at a time, so a second ask is told so rather than racing the first.
var cpuRecording atomic.Bool

// cpuSecond is one second of recording; tests shorten it.
var cpuSecond = time.Second

// cpuRefused says why r may not have the CPU profile, "" when it may: the
// same callers as the heap profile (heapRefused).
func cpuRefused(r *http.Request) string {
	return profileRefused(r, "CPU profile", "uni-switch cpu")
}

// cpuProfile answers CPUPath: the "cpu" profile (gzipped protobuf, as go
// tool pprof reads it) over ?seconds= (CPUSeconds by default), with
// CPUStats in CPUStatsHeader. It holds only its own request: the gateway
// goes on serving while it records, which is what it records. A caller
// that hangs up ends the recording, and nothing is sent.
func (s *Server) cpuProfile(w http.ResponseWriter, r *http.Request) {
	if why := cpuRefused(r); why != "" {
		log.Printf("refused %s %s from %s: %s", r.Method, r.URL.Path, r.RemoteAddr, why)
		writeError(w, provider.Chat, http.StatusForbidden, why)
		return
	}
	secs := CPUSeconds
	if v := r.URL.Query().Get("seconds"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < CPUMinSeconds || n > CPUMaxSeconds {
			writeError(w, provider.Chat, http.StatusBadRequest, "seconds is a whole number from "+strconv.Itoa(CPUMinSeconds)+" to "+strconv.Itoa(CPUMaxSeconds)+", not "+strconv.Quote(v))
			return
		}
		secs = n
	}
	if !cpuRecording.CompareAndSwap(false, true) {
		writeError(w, provider.Chat, http.StatusConflict, "uni-switch is recording a CPU profile already: wait for that one to finish, then run uni-switch cpu again")
		return
	}
	defer cpuRecording.Store(false)
	var buf bytes.Buffer
	before, known := processCPU()
	start := time.Now()
	if err := pprof.StartCPUProfile(&buf); err != nil {
		// another profiler of this process (a test's -cpuprofile)
		writeError(w, provider.Chat, http.StatusConflict, "uni-switch couldn't start a CPU profile: "+err.Error())
		return
	}
	t := time.NewTimer(time.Duration(secs) * cpuSecond)
	defer t.Stop()
	select {
	case <-t.C:
	case <-r.Context().Done():
		pprof.StopCPUProfile()
		log.Printf("CPU profile for %s ended early: the caller went away", r.RemoteAddr)
		return
	}
	goroutines := runtime.NumGoroutine()
	pprof.StopCPUProfile()
	took := time.Since(start)
	after, known2 := processCPU()
	st := CPUStats{
		PID:        os.Getpid(),
		Version:    Version,
		Window:     Window,
		Uptime:     time.Since(processStarted).Round(time.Second).String(),
		Seconds:    took.Round(time.Millisecond).Seconds(),
		CPUKnown:   known && known2,
		Goroutines: goroutines,
		NumCPU:     runtime.NumCPU(),
		GOMAXPROCS: runtime.GOMAXPROCS(0),
	}
	if exe, err := os.Executable(); err == nil {
		st.Program = filepath.Base(exe)
	}
	if st.CPUKnown {
		st.CPU = (after - before).Seconds()
		st.CPUTotal = after.Seconds()
	}
	stats, _ := json.Marshal(st)
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Disposition", `attachment; filename="uni-switch-cpu.pb.gz"`)
	h.Set("Cache-Control", "no-store")
	h.Set(CPUStatsHeader, string(stats))
	w.Write(buf.Bytes())
}
