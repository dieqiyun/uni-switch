package gui

import (
	"errors"
	"slices"
	"testing"
)

// Under Wayland the pages are drawn without accelerated compositing, which
// flickered (myxxts); under X11 too when no GPU can be reached, where
// compositing fell back to llvmpipe and took 459%–830% CPU (#2827). X11
// with a GPU is left alone, and so is a user's own setting.
func TestWebkitDefaults(t *testing.T) {
	for _, c := range []struct {
		name string
		env  map[string]string
		gpu  bool
		want string
	}{
		{"wayland", map[string]string{"XDG_SESSION_TYPE": "wayland"}, true, "1"},
		{"wayland display only", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, true, "1"},
		{"x11", map[string]string{"XDG_SESSION_TYPE": "x11"}, true, ""},
		{"x11 without a gpu (xrdp)", map[string]string{"XDG_SESSION_TYPE": "x11", "DISPLAY": ":10"}, false, "1"},
		{"user turned it on again", map[string]string{"XDG_SESSION_TYPE": "wayland", "WEBKIT_DISABLE_COMPOSITING_MODE": "0"}, true, "0"},
		{"user turned it on again without a gpu", map[string]string{"XDG_SESSION_TYPE": "x11", "WEBKIT_DISABLE_COMPOSITING_MODE": "0"}, false, "0"},
		{"user chose dmabuf off", map[string]string{"XDG_SESSION_TYPE": "wayland", "WEBKIT_DISABLE_DMABUF_RENDERER": "1"}, true, ""},
		{"user chose dmabuf off without a gpu", map[string]string{"XDG_SESSION_TYPE": "x11", "WEBKIT_DISABLE_DMABUF_RENDERER": "1"}, false, ""},
	} {
		env := map[string]string{}
		for k, v := range c.env {
			env[k] = v
		}
		webkitDefaultsFor(func(k string) string { return env[k] }, func(k, v string) error { env[k] = v; return nil }, func() bool { return c.gpu })
		if got := env["WEBKIT_DISABLE_COMPOSITING_MODE"]; got != c.want {
			t.Errorf("%s: WEBKIT_DISABLE_COMPOSITING_MODE=%q, want %q", c.name, got, c.want)
		}
	}
}

// A GPU is reachable when one render node opens: none there, or each one
// refused (the xrdp user outside the render group), is software drawing.
func TestRenderNodeOpens(t *testing.T) {
	denied := errors.New("permission denied")
	for _, c := range []struct {
		name  string
		nodes []string
		ok    map[string]bool
		want  bool
	}{
		{"no render node", nil, nil, false},
		{"every node refused", []string{"/dev/dri/renderD128", "/dev/dri/renderD129"}, nil, false},
		{"the second node opens", []string{"/dev/dri/renderD128", "/dev/dri/renderD129"}, map[string]bool{"/dev/dri/renderD129": true}, true},
	} {
		got := renderNodeOpens(c.nodes, func(p string) error {
			if c.ok[p] {
				return nil
			}
			return denied
		})
		if got != c.want {
			t.Errorf("%s: renderNodeOpens = %v, want %v", c.name, got, c.want)
		}
	}
}

// WebKitGTK's web process aborts on any video where autoaudiosink is
// missing (stock Omarchy: gst-plugins-base and -bad, no -good), so video is
// off only there; with GStreamer's folder not found, nothing is known and
// video stays on.
func TestGstAutodetect(t *testing.T) {
	for _, c := range []struct {
		name  string
		files []string
		want  bool
	}{
		{"plugins-good installed", []string{"/usr/lib/gstreamer-1.0/libgstcoreelements.so", "/usr/lib/gstreamer-1.0/libgstautodetect.so"}, true},
		{"stock omarchy", []string{"/usr/lib/gstreamer-1.0/libgstcoreelements.so", "/usr/lib/gstreamer-1.0/libgsttheora.so"}, false},
		{"good in a folder the variable names", []string{"/usr/lib/gstreamer-1.0/libgstcoreelements.so", "/opt/gst/libgstautodetect.so"}, true},
		{"no gstreamer folder found", nil, true},
	} {
		have := map[string]bool{}
		for _, f := range c.files {
			have[f] = true
		}
		if got := gstAutodetect([]string{"/opt/gst", "/usr/lib/gstreamer-1.0"}, func(p string) bool { return have[p] }); got != c.want {
			t.Errorf("%s: gstAutodetect = %v, want %v", c.name, got, c.want)
		}
	}
	env := map[string]string{"GST_PLUGIN_PATH_1_0": "/opt/gst:/opt/more", "GST_PLUGIN_SYSTEM_PATH": "/sys/gst"}
	dirs := gstPluginDirs(func(k string) string { return env[k] })
	for _, d := range []string{"/opt/gst", "/opt/more", "/sys/gst"} {
		if !slices.Contains(dirs, d) {
			t.Errorf("gstPluginDirs = %v, missing %s", dirs, d)
		}
	}
}
