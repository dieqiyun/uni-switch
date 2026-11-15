package gui

import (
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
)

// The Plugins page's Installed list is what was added, each row naming the
// subscriptions its plugin signs in to. Those names are what the plugins said
// the last time uni-switch asked them — but asking them is the whole of the
// providers RPC, which walks the plugins one at a time and has each fetch its
// vendor's model list over the network. The page here has no use for those
// models, so waiting on them made Installed take seconds to draw: one vendor
// after another, every time it was opened, the whole list held for the
// slowest of them.
//
// What the page reads instead is what plugin.Cached keeps: the names, without
// starting the host or asking anyone, with the plugins asked again in the
// background (Cached's own refresh) when a sign-in or the plugins changed
// since. So a row's names come from there, and asking the plugins is not on
// the way to drawing the list.

// pluginsSandbox points the uni-switch folders at a test's own, so the page reads
// a plugins.json the test wrote.
func pluginsSandbox(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	return appdir.Config()
}
