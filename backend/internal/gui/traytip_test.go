package gui

import "testing"

// A Linux tray's tooltip title is Pango markup in waybar: a bare & or <
// would lose all of it.
func TestSNITipIsSafeMarkup(t *testing.T) {
	got := sniTip("Claude Max 5h: 42% left\n\nR&D <team>")
	if want := "Claude Max 5h: 42% left\n\nR＆D ‹team›"; got != want {
		t.Fatalf("sniTip = %q, want %q", got, want)
	}
}
