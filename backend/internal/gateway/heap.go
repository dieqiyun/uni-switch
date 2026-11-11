package gateway

// GET /v1/uni-switch/heap: a Go heap profile of the process serving the
// gateway, for a report of uni-switch holding too much memory. A macOS
// `sample` of the app names none of Go's frames, so this is the one way
// to see what the Go side holds; `uni-switch heap` asks for it and writes the
// file the user sends.

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

// HeapPath is where the gateway answers a heap profile.
const HeapPath = "/v1/uni-switch/heap"

// HeapStatsHeader carries HeapStats, as JSON, beside the profile.
const HeapStatsHeader = "X-Uni-Switch-Heap-Stats"

// HeapStats is what Go's runtime says of the process's memory when the
// profile was taken. HeapAlloc and the others are read before the
// collection the profile runs; LiveHeap is HeapAlloc after it, what is
// still reachable. Sys is all Go took from the system, so a footprint far
// above it is memory outside Go (the webview, cgo).
type HeapStats struct {
	PID          int    `json:"pid"`
	Program      string `json:"program"`
	Version      string `json:"version"`
	Window       bool   `json:"window"` // the app's window or uni-switch web serves the gateway
	Uptime       string `json:"uptime"`
	HeapAlloc    uint64 `json:"heapAlloc"`
	HeapInuse    uint64 `json:"heapInuse"`
	HeapIdle     uint64 `json:"heapIdle"`
	HeapReleased uint64 `json:"heapReleased"`
	HeapSys      uint64 `json:"heapSys"`
	StackSys     uint64 `json:"stackSys"`
	Sys          uint64 `json:"sys"`
	LiveHeap     uint64 `json:"liveHeap"`
	NumGC        uint32 `json:"numGC"`
	Goroutines   int    `json:"goroutines"`
}

// processStarted is when this process began, for HeapStats' Uptime.
var processStarted = time.Now()

// heapProfile answers HeapPath: the "heap" profile (gzipped protobuf, as
// go tool pprof reads it) after a collection, with HeapStats in
// HeapStatsHeader.
func (s *Server) heapProfile(w http.ResponseWriter, r *http.Request) {
	if why := heapRefused(r); why != "" {
		log.Printf("refused %s %s from %s: %s", r.Method, r.URL.Path, r.RemoteAddr, why)
		writeError(w, provider.Chat, http.StatusForbidden, why)
		return
	}
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	goroutines := runtime.NumGoroutine()
	// what is still reachable, not what the next collection frees: the
	// profile's in-use numbers are as of the last collection
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	var buf bytes.Buffer
	if err := pprof.Lookup("heap").WriteTo(&buf, 0); err != nil {
		writeError(w, provider.Chat, http.StatusInternalServerError, "the heap profile couldn't be written: "+err.Error())
		return
	}
	exe, _ := os.Executable()
	stats, _ := json.Marshal(HeapStats{
		PID:          os.Getpid(),
		Program:      filepath.Base(exe),
		Version:      Version,
		Window:       Window,
		Uptime:       time.Since(processStarted).Round(time.Second).String(),
		HeapAlloc:    before.HeapAlloc,
		HeapInuse:    before.HeapInuse,
		HeapIdle:     before.HeapIdle,
		HeapReleased: before.HeapReleased,
		HeapSys:      before.HeapSys,
		StackSys:     before.StackSys,
		Sys:          before.Sys,
		LiveHeap:     after.HeapAlloc,
		NumGC:        after.NumGC,
		Goroutines:   goroutines,
	})
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Disposition", `attachment; filename="uni-switch-heap.pb.gz"`)
	h.Set("Cache-Control", "no-store")
	h.Set(HeapStatsHeader, string(stats))
	w.Write(buf.Bytes())
}

// heapRefused says why r may not have the heap profile, "" when it may.
// Only a program on this computer, calling it as localhost or a loopback
// address, does — never another machine, even with a gateway key (the
// profile is this computer's, not a shared gateway's), never anything
// carrying a browser's marks, whatever its Origin (a listed page, null, a
// localhost page), and never under another Host (DNS rebinding). lanGuard
// and corsGuard turn most of these away first; this holds where Handler
// is served without them too.
func heapRefused(r *http.Request) string {
	return profileRefused(r, "heap profile", "uni-switch heap")
}

// profileRefused is heapRefused for any profile of this process (cpuRefused
// too): what names the profile, cmd the command that asks for it.
func profileRefused(r *http.Request, what, cmd string) string {
	switch {
	case !local(r):
		return "uni-switch hands its " + what + " only to a program on this computer, over loopback: run " + cmd + " there"
	case fromBrowser(r):
		return "uni-switch doesn't hand its " + what + " to a web page: run " + cmd + " in a terminal"
	case !loopbackHost(r.Host):
		return "uni-switch hands its " + what + " only to a call to localhost or a loopback address, not " + r.Host + ": run " + cmd
	}
	return ""
}
