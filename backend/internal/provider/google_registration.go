package provider

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
)

// googleRegistered reads registrations owned or authorized by the user. No
// third-party registration is shipped or inferred from an access token.
func googleRegistered(app googleApp) googleApp {
	var registrations map[string]struct {
		ID     string `json:"clientId"`
		Secret string `json:"clientSecret"`
	}
	b, err := os.ReadFile(googleRegistrationPath())
	if err != nil || json.Unmarshal(b, &registrations) != nil {
		return app
	}
	c := registrations[app.agent]
	app.clientID, app.clientSecret = strings.TrimSpace(c.ID), strings.TrimSpace(c.Secret)
	return app
}

func googleRegistrationPath() string {
	return filepath.Join(appdir.Config(), "google-oauth.json")
}

func (app googleApp) oauthReady() error {
	if app.clientID == "" || app.clientSecret == "" || len(app.clientID) > 4096 || len(app.clientSecret) > 4096 ||
		strings.ContainsAny(app.clientID+app.clientSecret, "\r\n\x00") {
		return fmt.Errorf("%s needs your authorized Google OAuth registration: set %s.clientId and %s.clientSecret in %s; existing accounts without their original registration must sign in again", app.name, app.agent, app.agent, googleRegistrationPath())
	}
	return nil
}
