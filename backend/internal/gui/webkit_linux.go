package gui

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// webkitDefaults sets how WebKitGTK draws the pages before the app makes
// its first webview. Under Wayland its accelerated compositing flickers the
// window on a click or a hover (myxxts on Discord: Fedora 44, KDE, AMD
// Radeon 780M; Wails already turns DMA-BUF off for NVIDIA only), and with
// it off the pages draw steadily and, as they said, smoother. A user who
// set either variable keeps what they set; WEBKIT_DISABLE_COMPOSITING_MODE=0
// turns compositing back on.
//
// Under X11 with no GPU this process can draw with — an xrdp, VNC or Xvfb
// session, or a user outside the render group — the compositing falls back
// to Mesa's llvmpipe, which rasterises and blends every layer on the CPU
// with a thread per core: the Routing page took 459%–830% CPU there, and
// 23% with compositing off, looking the same (#2827, zhang-4411). So with
// no render node this process can open, compositing is off too.
func webkitDefaults() {
	webkitDefaultsFor(os.Getenv, os.Setenv, gpuReachable)
}

func webkitDefaultsFor(getenv func(string) string, setenv func(string, string) error, gpu func() bool) {
	if getenv("WEBKIT_DISABLE_COMPOSITING_MODE") != "" || getenv("WEBKIT_DISABLE_DMABUF_RENDERER") != "" {
		return
	}
	wayland := getenv("XDG_SESSION_TYPE") == "wayland" || getenv("WAYLAND_DISPLAY") != ""
	if !wayland && gpu() {
		return
	}
	setenv("WEBKIT_DISABLE_COMPOSITING_MODE", "1")
}

// gpuReachable reports whether this process can open a DRM render node,
// which is what EGL needs to draw on a GPU: none there, or none this user
// may open (libEGL's "failed to open /dev/dri/renderD128: Permission
// denied"), and WebKit's GL is software.
func gpuReachable() bool {
	nodes, _ := filepath.Glob("/dev/dri/renderD*")
	return renderNodeOpens(nodes, func(p string) error {
		f, err := os.OpenFile(p, os.O_RDWR, 0)
		if err == nil {
			f.Close()
		}
		return err
	})
}

func renderNodeOpens(nodes []string, open func(string) error) bool {
	for _, n := range nodes {
		if open(n) == nil {
			return true
		}
	}
	return false
}

// webkitPlaysVideo is whether WebKitGTK can play a <video> here. It gives
// every video, a muted one too, an audio sink it makes with autoaudiosink,
// from gst-plugins-good; where that isn't installed (stock Omarchy has only
// -base and -bad) it gets none, and its web process aborts a moment after
// the video starts loading, which leaves the window blank. GStreamer keeps
// the element in libgstautodetect.so. A system with no plugin folder uni-switch
// knows of says nothing either way, and plays.
var webkitPlaysVideo = sync.OnceValue(func() bool {
	return gstAutodetect(gstPluginDirs(os.Getenv), func(p string) bool { _, err := os.Stat(p); return err == nil })
})

// gstPluginDirs is where GStreamer looks for plugins: the folders its
// variables name, and the system's own.
func gstPluginDirs(getenv func(string) string) []string {
	var dirs []string
	for _, k := range []string{"GST_PLUGIN_SYSTEM_PATH_1_0", "GST_PLUGIN_SYSTEM_PATH", "GST_PLUGIN_PATH_1_0", "GST_PLUGIN_PATH"} {
		for _, d := range strings.Split(getenv(k), ":") {
			if d != "" {
				dirs = append(dirs, d)
			}
		}
	}
	for _, g := range []string{"/usr/lib*/gstreamer-1.0", "/usr/lib*/*/gstreamer-1.0", "/usr/local/lib*/gstreamer-1.0", "/usr/local/lib*/*/gstreamer-1.0"} {
		m, _ := filepath.Glob(g)
		dirs = append(dirs, m...)
	}
	return dirs
}

// gstAutodetect is false only when GStreamer's core plugins are there and
// autoaudiosink is in none of the folders.
func gstAutodetect(dirs []string, exists func(string) bool) bool {
	found := false
	for _, d := range dirs {
		if exists(filepath.Join(d, "libgstautodetect.so")) {
			return true
		}
		found = found || exists(filepath.Join(d, "libgstcoreelements.so"))
	}
	return !found
}
