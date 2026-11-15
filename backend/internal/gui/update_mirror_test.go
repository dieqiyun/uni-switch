package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"github.com/dieqiyun/uni-switch/backend/internal/update"
)

// Settings › About › Download source (#893): the mirror is set and taken
// away on its own, the same setting `uni-switch update mirror` writes, a full
// https:// address only, and the Settings page's save keeps it. A download
// that fails through it says so (the page's mirror), which a check clears.
func TestUpdateMirrorSetting(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		t.Setenv(k, "")
	}
	call := func(method, path, body string) (int, string) {
		t.Helper()
		rec := httptest.NewRecorder()
		Handler(nil, nil).ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec.Code, rec.Body.String()
	}
	const m = "https://mirror.example/gh/"
	code, body := call("POST", "/api/settings/update-mirror", `{"mirror":" `+m+` "}`)
	if code != http.StatusOK || settings.Load().UpdateMirror != m || !strings.Contains(body, `"updateMirror":"`+m+`"`) {
		t.Fatalf("set: %d %s, saved %q", code, body, settings.Load().UpdateMirror)
	}
	for _, bad := range []string{"http://mirror.example/", "mirror.example", "ftp://mirror.example/", "https://"} {
		if code, body = call("POST", "/api/settings/update-mirror", `{"mirror":"`+bad+`"}`); code == http.StatusOK || settings.Load().UpdateMirror != m || !strings.Contains(body, "full https://") {
			t.Errorf("%s: %d %s, saved %q", bad, code, body, settings.Load().UpdateMirror)
		}
	}
	// the Settings page's save, which doesn't send it, keeps it
	if code, body = call("POST", "/api/settings", `{"theme":"dark","updateMirror":""}`); code != http.StatusOK || settings.Load().UpdateMirror != m {
		t.Fatalf("save: %d %s, mirror %q", code, body, settings.Load().UpdateMirror)
	}
	// GitHub again
	if code, body = call("POST", "/api/settings/update-mirror", `{"mirror":""}`); code != http.StatusOK || settings.Load().UpdateMirror != "" || strings.Contains(body, `"updateMirror"`) {
		t.Fatalf("off: %d %s", code, body)
	}

	// a download through a mirror that answers 404: the error is the mirror's
	mirror := httptest.NewServer(http.NotFoundHandler())
	defer mirror.Close()
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(update.Release{Version: "0.1.11", Assets: map[string]update.Asset{
			update.BinaryAsset(): {URL: "https://github.com/dieqiyun/uni-switch/releases/download/v0.1.11/x", SHA256: strings.Repeat("0", 64)},
		}})
	}))
	defer feed.Close()
	t.Setenv("UNI_SWITCH_UPDATE_FEED", feed.URL)
	if err := settings.Save(settings.Settings{UpdateMirror: mirror.URL + "/"}); err != nil {
		t.Fatal(err)
	}
	old := Version
	Version = "0.1.10"
	defer func() { Version = old }()
	exe, err := update.Executable()
	if err != nil {
		t.Fatal(err)
	}
	u := &updater{exe: exe}
	u.self, _ = os.Stat(exe)
	defer os.Remove(exe + ".new")
	u.check()
	j := u.json()
	if j.State != "error" || !j.Retry || j.Mirror != mirror.URL+"/" || j.Network || !strings.Contains(j.Error, mirror.URL) {
		t.Fatalf("through the mirror: %+v", j)
	}
	// the next check starts afresh: no mirror said while it runs
	if !u.begin() || u.json().Mirror != "" {
		t.Fatalf("begin kept the mirror: %+v", u.json())
	}
}
