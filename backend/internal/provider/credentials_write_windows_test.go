package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// #2239: on Windows another program holding auth.json open for a moment
// (no write or delete sharing, as most do) refuses a write. A truncating
// write was refused and its error dropped, so the rotated tokens never
// reached the disk while the refresh token on it was spent, and the next
// refresh, uni-switch's or Codex CLI's, was refused (refresh_token_reused).
// The rename that replaces it waits the holder out.
func TestCodexRefreshOutlastsAHeldAuthJSON(t *testing.T) {
	isolate(t)
	fakeCodexRefresh(t)
	path, _ := dueCodexAuth(t)
	name, _ := syscall.UTF16PtrFromString(path)
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		defer close(released)
		time.Sleep(100 * time.Millisecond)
		syscall.CloseHandle(h)
	}()
	defer func() { <-released }()
	if _, _, err := codexToken(context.Background(), filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	<-released
	if b, err := os.ReadFile(path); err != nil || !strings.Contains(string(b), `"r-work-2"`) {
		t.Fatalf("the rotated tokens are not on the disk, only the spent one's: %s (%v)", b, err)
	}
}
