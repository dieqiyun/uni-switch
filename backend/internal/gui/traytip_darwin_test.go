//go:build darwin && cgo && !nogui

package gui

import "testing"

// NextCommit on Discord: hovering the Mac's menu bar item showed no
// tooltip at all, so neither today's tokens split into input, output and
// cache nor the quotas were ever seen there. Wails' SetTooltip does nothing
// on macOS; the tooltip is set on the item's button, its lines kept, and a
// tooltip set before the item is up is tried again (false).
func TestMacTrayTipIsSet(t *testing.T) {
	var got []string
	ok := false
	old := trayTipNative
	trayTipNative = func(tip string) bool { got = append(got, tip); return ok }
	t.Cleanup(func() { trayTipNative = old })

	tip := "Today in uni-switch\n6.3M tokens (912K in · 318K out · 5.1M cached)\n≈$3.40+\n152 requests"
	if setTrayTip(nil, tip) {
		t.Error("set before the item is up: want false, to be tried again")
	}
	ok = true
	if !setTrayTip(nil, tip) {
		t.Error("set on the item: want true")
	}
	if len(got) != 2 || got[0] != tip || got[1] != tip {
		t.Fatalf("native tooltip set %q, want %q twice", got, tip)
	}

	// what a status bar button shows: the text whole, every line of it
	if shown := trayTipShown(tip); shown != tip {
		t.Errorf("button shows %q, want %q", shown, tip)
	}
	if shown := trayTipShown(""); shown != "" {
		t.Errorf("empty tip shows %q", shown)
	}
}
