package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGoogleOAuthRequiresUserConfiguration(t *testing.T) {
	if geminiApp.clientID != "" || geminiApp.clientSecret != "" || antigravityApp.clientID != "" || antigravityApp.clientSecret != "" {
		t.Fatal("Google OAuth registrations must not be bundled")
	}
	for _, agent := range []string{"gemini", "antigravity"} {
		t.Run(agent, func(t *testing.T) {
			prefix := "UNI_SWITCH_" + strings.ToUpper(agent) + "_OAUTH_"
			t.Setenv(prefix+"CLIENT_ID", "")
			t.Setenv(prefix+"CLIENT_SECRET", "")
			s := &signInFlow{st: SignInState{Agent: agent}}
			if err := s.begin(); err == nil || !strings.Contains(err.Error(), prefix+"CLIENT_ID") || s.srv != nil || s.st.URL != "" {
				t.Fatalf("unconfigured sign-in must fail before opening a callback or browser URL: %v", err)
			}
			app, _ := googleAppOf(agent)
			g := googleAccount{app: app, auth: googleAuth{RefreshToken: "qa-not-sent"}}
			if _, err := g.token(context.Background()); err == nil || !strings.Contains(err.Error(), prefix+"CLIENT_ID") {
				t.Fatalf("unconfigured refresh must fail locally: %v", err)
			}
			t.Setenv(prefix+"CLIENT_ID", " qa-client ")
			t.Setenv(prefix+"CLIENT_SECRET", " qa-secret ")
			app, _ = googleAppOf(agent)
			if app.clientID != "qa-client" || app.clientSecret != "qa-secret" || app.requireOAuth() != nil {
				t.Fatal("user OAuth configuration was not applied")
			}
		})
	}
}

func TestGoogleOAuthSavedClientSurvivesEnvironmentChange(t *testing.T) {
	t.Setenv("UNI_SWITCH_GEMINI_OAUTH_CLIENT_ID", "qa-new-client")
	t.Setenv("UNI_SWITCH_GEMINI_OAUTH_CLIENT_SECRET", "qa-new-secret")
	auth, _ := json.Marshal(googleAuth{ClientID: "qa-original-client", ClientSecret: "qa-original-secret", RefreshToken: "qa-saved-oauth-refresh"})
	g, ok := googleSaved("gemini", savedLogin{Agent: "gemini", User: "qa@example.test", Auth: auth})
	if !ok || g.app.clientID != "qa-original-client" || g.app.clientSecret != "qa-original-secret" {
		t.Fatal("saved account must retain the client that minted its refresh token")
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("client_id") != "qa-original-client" || r.Form.Get("client_secret") != "qa-original-secret" {
			t.Error("refresh used a different OAuth registration")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"qa-refreshed","expires_in":3600}`))
	}))
	defer upstream.Close()
	old := googleTokenURL
	googleTokenURL = upstream.URL
	defer func() { googleTokenURL = old; resetGoogleState() }()
	resetGoogleState()
	if token, err := g.token(context.Background()); err != nil || token != "qa-refreshed" {
		t.Fatalf("saved OAuth registration cannot refresh: %v", err)
	}
}
