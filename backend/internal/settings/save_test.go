package settings

import (
	"bytes"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
)

// A default or partially decoded display must not become the new file.
func TestSaveKeepsCorruptSettings(t *testing.T) {
	const token = "SYNTHETIC_PRIVATE_TOKEN"
	for _, tc := range []struct{ name, body string }{
		{"truncated", `{"theme":"dark","githubToken":"` + token + `"`},
		{"theme type", `{"theme":7,"githubToken":"` + token + `"}`},
		{"proxy type", `{"proxy":7,"githubToken":"` + token + `"}`},
		{"nested type", `{"window":{"width":"invalid"},"githubToken":"` + token + `"}`},
		{"BOM truncated", "\ufeff" + `{"theme":"dark","githubToken":"` + token + `"`},
		{"BOM type", "\ufeff" + `{"theme":7,"githubToken":"` + token + `"}`},
		{"non-JSON whitespace", "\u00a0" + `{"theme":"dark","githubToken":"` + token + `"}`},
		{"array", `[]`},
		{"trailing JSON", `{"theme":"dark"} {}`},
	} {
		for _, loaded := range []bool{false, true} {
			kind := "fresh"
			if loaded {
				kind = "loaded"
			}
			t.Run(tc.name+"/"+kind, func(t *testing.T) {
				t.Setenv("XDG_CONFIG_HOME", t.TempDir())
				if err := os.MkdirAll(Dir(), 0o755); err != nil {
					t.Fatal(err)
				}
				original := []byte(tc.body)
				if err := os.WriteFile(Path(), original, 0o640); err != nil {
					t.Fatal(err)
				}
				before, err := os.Stat(Path())
				if err != nil {
					t.Fatal(err)
				}
				s := Settings{Theme: "light"}
				if loaded {
					s = Load()
				}
				s.NoAutoUpdate = true
				if err := Save(s); err == nil {
					t.Error("corrupt settings were accepted")
				} else if !strings.Contains(err.Error(), Path()) || strings.Contains(err.Error(), token) {
					t.Errorf("error must name the file without exposing its contents: %v", err)
				}
				after, err := os.ReadFile(Path())
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(after, original) {
					t.Error("the original settings were overwritten")
				}
				fi, err := os.Stat(Path())
				if err != nil {
					t.Fatal(err)
				}
				if fi.Mode().Perm() != before.Mode().Perm() {
					t.Errorf("refused save changed permissions: %v, was %v", fi.Mode(), before.Mode())
				}
			})
		}
	}
}

func TestSaveRecoversEmptySettings(t *testing.T) {
	for _, body := range []string{"", " \t\r\n", "\ufeff", "\ufeff \t\r\n"} {
		t.Run(body, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			if err := os.MkdirAll(Dir(), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(Path(), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			s := Load()
			s.Theme, s.NoAutoUpdate = "light", true
			if err := Save(s); err != nil {
				t.Fatal(err)
			}
			if got := Load(); got.Theme != "light" || !got.NoAutoUpdate {
				t.Fatal("the preference change was not saved to the empty file")
			}
		})
	}
}

func TestSettingsBOM(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	const token = "SYNTHETIC_PRIVATE_TOKEN"
	body := "\ufeff" + ` {"theme":"dark","proxy":"direct","lang":"zh","githubToken":"` + token + `"}`
	if err := os.WriteFile(Path(), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Load()
	if s.Theme != "dark" || s.Proxy != "direct" || s.Lang != "zh" || s.GitHubToken != token {
		t.Error("loading a BOM file lost existing fields")
	}
	s.NoAutoUpdate = true
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if !got.NoAutoUpdate || got.Theme != "dark" || got.Proxy != "direct" || got.Lang != "zh" || got.GitHubToken != token {
		t.Fatal("editing a BOM file lost existing fields")
	}
}

func TestSaveKeepsUnreadableSettings(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows read restrictions are covered by TestSaveKeepsReadBlockedSettings")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"theme":"dark","githubToken":"SYNTHETIC_PRIVATE_TOKEN"}`)
	if err := os.WriteFile(Path(), original, 0o200); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(Path()); !errors.Is(err, os.ErrPermission) {
		t.Skipf("this process can read a write-only file: %v", err)
	}
	err := Save(Settings{Theme: "light"})
	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("Save returned %v, want the read permission error", err)
	}
	if fi, err := os.Stat(Path()); err != nil || fi.Mode().Perm() != 0o200 {
		t.Errorf("refused save changed the write-only file's mode: %v, %v", fi, err)
	}
	if err := os.Chmod(Path(), 0o600); err != nil {
		t.Fatal(err)
	}
	if after, err := os.ReadFile(Path()); err != nil || !bytes.Equal(after, original) {
		t.Errorf("the unreadable settings were overwritten: %v", err)
	}
}
