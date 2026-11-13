package gui

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// Account automation has dedicated saves so editing one account cannot alter another.
func accountAutomationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/settings/account-warmup", func(rw http.ResponseWriter, r *http.Request) {
		var in struct {
			Agent, User string
			settings.AccountWarmup
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(rw, err)
			return
		}
		in.User = strings.TrimSpace(in.User)
		if (in.Agent != "codex" && in.Agent != "claude") || !slices.ContainsFunc(provider.Logins(in.Agent), func(l provider.Login) bool { return strings.EqualFold(l.User, in.User) }) {
			fail(rw, fmt.Errorf("no such subscription account"))
			return
		}
		s, err := settings.LoadChecked()
		if err != nil {
			fail(rw, err)
			return
		}
		s.AccountWarmups = maps.Clone(s.AccountWarmups)
		if s.AccountWarmups == nil {
			s.AccountWarmups = map[string]settings.AccountWarmup{}
		}
		s.AccountWarmups[in.Agent+"|"+strings.ToLower(in.User)] = in.AccountWarmup
		if err := settings.Save(s); err != nil {
			fail(rw, err)
			return
		}
		writeJSON(rw, settingsState())
	})
	mux.HandleFunc("POST /api/settings/account-checkin", func(rw http.ResponseWriter, r *http.Request) {
		var in struct {
			Key string
			On  bool
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(rw, err)
			return
		}
		if !slices.ContainsFunc(provider.CheckinAccounts(), func(a provider.CheckinAccount) bool { return a.Key == in.Key }) {
			fail(rw, fmt.Errorf("no such check-in account"))
			return
		}
		s, err := settings.LoadChecked()
		if err != nil {
			fail(rw, err)
			return
		}
		s.AccountCheckins = maps.Clone(s.AccountCheckins)
		if s.AccountCheckins == nil {
			s.AccountCheckins = map[string]bool{}
		}
		s.AccountCheckins[in.Key] = in.On
		if err := settings.Save(s); err != nil {
			fail(rw, err)
			return
		}
		writeJSON(rw, settingsState())
	})
}
