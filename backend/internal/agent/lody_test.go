package agent

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLodyLink(t *testing.T) {
	for _, base := range []string{"http://127.0.0.1:3425", "http://localhost:4567", "http://[::1]:4567"} {
		t.Run(base, func(t *testing.T) {
			u, err := url.Parse(LodyLink(base))
			if err != nil {
				t.Fatal(err)
			}
			if u.Scheme != "lody" || u.Host != "provider" || u.Path != "/import" || u.Query().Get("v") != "1" || len(u.Query()) != 2 {
				t.Fatalf("invalid import URL: %s", u)
			}
			data, err := base64.RawURLEncoding.DecodeString(u.Query().Get("data"))
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{
				"kind": "custom", "id": "uni-switch", "name": "uni-switch",
				"auth": map[string]any{"method": "apiKey", "apiKey": "uni-switch-lody"},
				"endpoints": []any{
					map[string]any{"protocol": "anthropic-messages", "baseUrl": base, "targets": []any{"claude-code"}, "modelsUrl": base + "/v1/models"},
					map[string]any{"protocol": "openai-responses", "baseUrl": base + "/v1", "targets": []any{"codex"}, "modelsUrl": base + "/v1/models"},
					map[string]any{"protocol": "openai-chat", "baseUrl": base + "/v1", "targets": []any{"pi", "dsh", "kimi-code", "grok", "bub"}, "modelsUrl": base + "/v1/models"},
				},
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("wrong Lody envelope: %s", data)
			}
			if LodyLink(base+"/") != LodyLink(base) {
				t.Fatal("trailing slash changed the endpoint")
			}
		})
	}
	for raw, canonical := range map[string]string{"http://localhost:80": "http://localhost", "http://127.0.0.1:03425": "http://127.0.0.1:3425"} {
		if LodyLink(raw) != LodyLink(canonical) {
			t.Errorf("%s must use canonical origin %s", raw, canonical)
		}
	}
	for _, base := range []string{"", "https://127.0.0.1:3425", "http://192.168.1.2:3425", "http://example.com", "http://127.0.0.1.evil.test", "http://user:secret@127.0.0.1", "http://127.0.0.1/v1", "http://localhost?token=secret", "http://localhost#fragment", "http://localhost?", "http://localhost:0", "http://localhost:65536", "http://localhost:", "http://::1"} {
		if got := LodyLink(base); got != "" {
			t.Errorf("unsupported gateway %q generated %s", base, got)
		}
	}
}

func TestLodyDetectionAndRegistration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("APPDATA", filepath.Join(home, "config"))
	a := lody(home)
	if a.Detected() {
		t.Fatal("detected Lody in an empty home")
	}
	for _, name := range []string{".lody", ".lody-oss"} {
		dir := filepath.Join(home, name)
		// a bare folder of that name another tool made isn't Lody
		if err := os.MkdirAll(filepath.Join(dir, "bin"), 0700); err != nil {
			t.Fatal(err)
		}
		if a.Detected() {
			t.Fatalf("a %s without Lody's own entries counted as Lody", name)
		}
		for _, m := range lodyMarks {
			if err := os.WriteFile(filepath.Join(dir, m), nil, 0600); err != nil {
				t.Fatal(err)
			}
			if !a.Detected() {
				t.Fatalf("missed %s with %s", name, m)
			}
			if err := os.Remove(filepath.Join(dir, m)); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range lodyNames {
		dir := filepath.Join(cfg, name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if !a.Detected() {
			t.Fatalf("missed Lody's data folder %s", dir)
		}
		if err := os.Remove(dir); err != nil {
			t.Fatal(err)
		}
	}
	if a.Detected() {
		t.Fatal("still detected after its folders went")
	}
	if a.Added != nil || len(a.Fields) != 0 || a.Sync != nil || a.Unwire != nil {
		t.Fatal("import adapter must not infer status or edit Lody settings")
	}
	for _, a := range All() {
		if a.ID == "lody" {
			if a.Import == nil || a.Name != "Lody" || a.Icon != "lody" {
				t.Fatalf("bad registered adapter: %+v", a)
			}
			return
		}
	}
	t.Fatal("Lody missing from Agents")
}

// A Lody.app is Lody's only with its bundle id (ai.lody.desktop on the
// real app) or a plist that says nothing; another vendor's Lody.app isn't.
func TestLodyBundle(t *testing.T) {
	plist := func(id string) string {
		return `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>` + id + `</string></dict></plist>`
	}
	for _, c := range []struct {
		name, plist string
		want        bool
	}{
		{"Lody", plist("ai.lody.desktop"), true},
		{"Lody Nightly", plist("ai.lody.desktop.nightly"), true},
		{"Lody", "bplist00", true},
		{"Lody", plist("com.example.lody"), false},
		{"Lody", plist("ai.lodyx.desktop"), false},
		{"Lodyssey", plist("ai.lody.desktop"), false},
	} {
		dir := t.TempDir()
		contents := filepath.Join(dir, c.name+".app", "Contents")
		if err := os.MkdirAll(contents, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(c.plist), 0600); err != nil {
			t.Fatal(err)
		}
		if got := lodyBundle([]string{dir}); got != c.want {
			t.Errorf("%s.app with %q: %v, want %v", c.name, c.plist, got, c.want)
		}
	}
	if lodyBundle([]string{t.TempDir()}) {
		t.Error("no bundle counted as Lody")
	}
}

func loopbackHost(h string) bool {
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// Lody on Linux is run with its import link as its desktop entry says.
// Its AppImage makes itself the default only for ai.lody.stable://, so
// lody:// reaches it only through a desktop database that nothing builds
// without update-desktop-database, and xdg-open turned the link away
// while Lody ran (Atlas, 10-25: "failed to open uri"). The entries are
// the ones Lody 0.108.1 writes: its AppImage's, with the path quoted and
// --no-sandbox kept, and its .deb's.

func TestDesktopEntryArguments(t *testing.T) {
	for _, c := range []struct {
		entry string
		want  []string
	}{
		{"[Desktop Entry]\nExec=/bin/sh -c 'x' %U\n", []string{"/bin/sh", "-c", "'x'", "L"}},
		{"[Desktop Entry]\nExec=\"/bin/sh\" \"100%%\" %u %f %i\n", []string{"/bin/sh", "100%", "L"}},
		{"[Desktop Entry]\nName=Lody\n", nil},
	} {
		if got := desktopArgs(c.entry, "L"); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: %q, want %q", c.entry, got, c.want)
		}
	}
}
