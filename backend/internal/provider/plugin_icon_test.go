package provider

import (
	"context"
	"encoding/base64"

	"path/filepath"
	"strings"

	"testing"
	"time"
)

// yetone: 非官方插件显示不出来 logo. A repository's https picture not fetched
// before is waited for, so the GitHub list's first answer has it; one slower
// than the wait shows the next time, and the wait ends when it does.
func TestRepoIconsWaitForFirstFetch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	slow := make(chan struct{})
	// the fetch left waiting ends with the test
	t.Cleanup(func() {
		close(slow)
		for i := 0; i < 200; i++ {
			plugIcons.Lock()
			n := len(plugIcons.busy)
			plugIcons.Unlock()
			if n == 0 {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Error("the slow fetch didn't end")
	})
	was := fetchPluginIcon
	t.Cleanup(func() { fetchPluginIcon = was })
	fetchPluginIcon = func(ctx context.Context, u string) (string, error) {
		if strings.Contains(u, "slow") {
			select {
			case <-slow:
			case <-ctx.Done():
			}
			return "", ctx.Err()
		}
		time.Sleep(100 * time.Millisecond)
		return StoreIcon(png)
	}

	data := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	said := []string{"https://gaccode.example/favicon.png", "", data}
	got := RepoIcons(said, 5*time.Second)
	if !strings.HasPrefix(got[0], "file:") || got[1] != "" || !strings.HasPrefix(got[2], "file:") {
		t.Fatalf("first answer = %q", got)
	}

	// The slow fetch is held until the test ends, so RepoIcons coming back at
	// all is its wait ending it (#1617); the floor is that it did wait, which
	// a busy machine only lengthens. The deadline only turns a wait past the
	// 300ms into a FAIL, and is short of the fetch's own 30s.
	type answer struct {
		icon string
		took time.Duration
	}
	answered := make(chan answer, 1)
	go func() {
		start := time.Now()
		got := RepoIcons([]string{"https://slow.example/icon.png"}, 300*time.Millisecond)
		answered <- answer{got[0], time.Since(start)}
	}()
	select {
	case a := <-answered:
		if a.icon != "" {
			t.Fatalf("a fetch never finished = %q", a.icon)
		}
		if a.took < 250*time.Millisecond {
			t.Fatalf("waited %v for a 300ms wait", a.took)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RepoIcons waited past its 300ms for the held fetch")
	}
}
