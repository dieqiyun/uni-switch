package appdir

import (
	"os"
	"path/filepath"
	"testing"
)

// The note beside a Windows uni-switch installed from a download names the
// webview profile it keeps (update.Settle); a missing note, or one that
// doesn't name an absolute folder, leaves WebView2's own default.
func TestWebViewNote(t *testing.T) {
	dir := t.TempDir()
	note := filepath.Join(dir, WebViewNote)
	if got := noted(note); got != "" {
		t.Fatalf("no note read as %q", got)
	}
	want := filepath.Join(dir, "Roaming", "uni-switch-windows-amd64.exe")
	if err := os.WriteFile(note, []byte(want+"\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := noted(note); got != want {
		t.Fatalf("note read as %q, want %q", got, want)
	}
	if err := os.WriteFile(note, []byte("uni-switch-windows-amd64.exe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := noted(note); got != "" {
		t.Fatalf("a relative note read as %q", got)
	}
}
