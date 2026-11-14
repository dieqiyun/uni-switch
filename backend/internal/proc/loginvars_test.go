//go:build !windows

package proc

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// LoginVars reads any variable the login shell exports, set-but-empty
// apart from unset, never one it doesn't export, and never sets one in
// uni-switch's environment. An answer is kept, so a second ask doesn't run
// the shell again; another SHELL asks again.
func TestLoginVars(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	old := shellDirs
	shellDirs = func() []string { return nil }
	t.Cleanup(func() { shellDirs = old })
	runs := filepath.Join(home, "runs")
	sh := filepath.Join(home, "sh")
	testenv.Program(t, sh, "#!/bin/sh\necho ran >> "+runs+"\necho 'hello from .zshrc'\n"+
		"export LV_TOKEN='a b\"c$d' LV_EMPTY=\nLV_HIDDEN=x\neval \"$2\"\n")
	t.Setenv("SHELL", sh)
	for _, v := range []string{"LV_TOKEN", "LV_EMPTY", "LV_HIDDEN", "LV_NONE"} {
		t.Setenv(v, "")
		os.Unsetenv(v)
	}
	got := LoginVars([]string{"LV_TOKEN", "LV_EMPTY", "LV_HIDDEN", "LV_NONE", "bad name", "1X"})
	if v, ok := got["LV_TOKEN"]; !ok || v != `a b"c$d` {
		t.Errorf("LV_TOKEN = %q, %v", v, ok)
	}
	if v, ok := got["LV_EMPTY"]; !ok || v != "" {
		t.Errorf("LV_EMPTY = %q, %v; want set and empty", v, ok)
	}
	for _, n := range []string{"LV_HIDDEN", "LV_NONE", "bad name", "1X"} {
		if _, ok := got[n]; ok {
			t.Errorf("%s is set: %q", n, got[n])
		}
	}
	if _, ok := os.LookupEnv("LV_TOKEN"); ok {
		t.Error("LV_TOKEN set in uni-switch's environment")
	}
	b, _ := os.ReadFile(runs)
	n := len(b)
	if again := LoginVars([]string{"LV_TOKEN", "LV_NONE"}); again["LV_TOKEN"] != `a b"c$d` {
		t.Errorf("kept answer %v", again)
	}
	if b, _ = os.ReadFile(runs); len(b) != n {
		t.Error("the shell was asked again within the minute")
	}
	sh2 := filepath.Join(home, "sh2")
	testenv.Program(t, sh2, "#!/bin/sh\nexport LV_TOKEN=other\neval \"$2\"\n")
	t.Setenv("SHELL", sh2)
	if v := LoginVars([]string{"LV_TOKEN"})["LV_TOKEN"]; v != "other" {
		t.Errorf("another shell's answer: %q", v)
	}
}
