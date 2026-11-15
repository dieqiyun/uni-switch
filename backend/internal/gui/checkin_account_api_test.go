package gui

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agentenv"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// One account's daily check-in is turned off and on by itself (#1902,
// magicLaLa: 根据每个账号设置是否开启自动签到): off adds it to
// settings.CheckinOff once, on takes it out, another account's entry and
// the vendor's switch are left as they were, and a save of the Settings
// page, which doesn't send it, keeps it.
func TestCheckinAccountSetting(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	t.Setenv("APPDATA", filepath.Join(h, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(h, "AppData", "Local"))
	for _, v := range agentenv.Vars {
		t.Setenv(v, "")
	}
	s := settings.Load()
	s.WorkBuddyCheckin, s.CheckinOff = true, []string{"workbuddy|u2"}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	handler := Handler(nil, nil)
	post := func(path, body string) {
		t.Helper()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s: %d %s", path, body, rec.Code, rec.Body)
		}
	}
	off := func(want ...string) {
		t.Helper()
		got := settings.Load()
		if !slices.Equal(got.CheckinOff, want) || !got.WorkBuddyCheckin {
			t.Fatalf("checkinOff %q (want %q), workbuddyCheckin %v", got.CheckinOff, want, got.WorkBuddyCheckin)
		}
	}
	post("/api/settings/checkin-account", `{"key":"workbuddy|u1","on":false}`)
	off("workbuddy|u2", "workbuddy|u1")
	post("/api/settings/checkin-account", `{"key":"workbuddy|u1","on":false}`)
	off("workbuddy|u2", "workbuddy|u1")
	post("/api/settings", `{"theme":"dark","workbuddyCheckin":true}`)
	off("workbuddy|u2", "workbuddy|u1")
	post("/api/settings/checkin-account", `{"key":"workbuddy|u1","on":true}`)
	off("workbuddy|u2")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("POST", "/api/settings/checkin-account", strings.NewReader(`{"on":false}`)))
	if rec.Code == http.StatusOK {
		t.Fatal("no account, yet saved")
	}
}
