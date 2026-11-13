package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeGoogleRegistrations(t *testing.T, id string) {
	t.Helper()
	path := googleRegistrationPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{
		"gemini":      map[string]string{"clientId": id, "clientSecret": "synthetic-gemini-secret"},
		"antigravity": map[string]string{"clientId": "synthetic-antigravity-client", "clientSecret": "synthetic-antigravity-secret"},
	})
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestGoogleRegistrationRequiredBeforeSignInOrImport(t *testing.T) {
	googleSandbox(t, &fakeGoogle{})
	if err := os.Remove(googleRegistrationPath()); err != nil {
		t.Fatal(err)
	}
	for _, agent := range []string{"gemini", "antigravity"} {
		st, err := StartSignIn(agent)
		if err == nil {
			CancelSignIn(st.ID)
			t.Fatalf("%s started without a registration", agent)
		}
		if !strings.Contains(err.Error(), "google-oauth.json") {
			t.Fatalf("missing setup instructions: %v", err)
		}
	}
	if _, err := ImportGoogleAccounts(context.Background(), "antigravity", []string{"synthetic-refresh"}); err == nil {
		t.Fatal("import reached Google without a registration")
	}
	if err := os.WriteFile(googleRegistrationPath(), []byte("{invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	app, _ := googleAppOf("gemini")
	if app.oauthReady() == nil {
		t.Fatal("malformed registration accepted")
	}
}

func TestGoogleSavedRegistrationSurvivesConfigurationChange(t *testing.T) {
	googleSandbox(t, &fakeGoogle{})
	writeGoogleRegistrations(t, "synthetic-original-client")
	if err := addGoogleLogin("gemini", "me@example.com", "", googleAuth{RefreshToken: "synthetic-refresh"}); err != nil {
		t.Fatal(err)
	}
	writeGoogleRegistrations(t, "synthetic-replacement-client")
	logins := googleLogins("gemini")
	if len(logins) != 1 {
		t.Fatalf("saved accounts: %d", len(logins))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("client_id") != "synthetic-original-client" || r.Form.Get("client_secret") != "synthetic-gemini-secret" {
			t.Error("refresh used a different registration")
		}
		io.WriteString(w, `{"access_token":"synthetic-access","expires_in":3600}`)
	}))
	defer srv.Close()
	googleTokenURL = srv.URL
	if _, err := logins[0].acct.token(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Old tokens with no recorded registration must not silently use the new one.
	b, _ := json.Marshal(googleAuth{RefreshToken: "legacy-refresh"})
	old, ok := googleSaved("gemini", savedLogin{Agent: "gemini", User: "legacy@example.com", Auth: b})
	if !ok {
		t.Fatal("legacy account disappeared")
	}
	if _, err := old.token(context.Background()); err == nil {
		t.Fatal("legacy token refreshed with an unrelated registration")
	}
}

func TestGoogleSignInCapturesRegistration(t *testing.T) {
	googleSandbox(t, &fakeGoogle{})
	writeGoogleRegistrations(t, "synthetic-start-client")
	st, err := StartSignIn("gemini")
	if err != nil {
		t.Fatal(err)
	}
	defer CancelSignIn(st.ID)
	u, _ := url.Parse(st.URL)
	if u.Query().Get("client_id") != "synthetic-start-client" {
		t.Fatal("authorization used another registration")
	}
	writeGoogleRegistrations(t, "synthetic-changed-client")
	// Follow the browser callback through the code exchange, then deliberately
	// refuse the fake code so no account lookup can reach the real service.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("client_id") != "synthetic-start-client" {
			t.Error("callback changed the authorization's registration")
		}
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	googleTokenURL = srv.URL
	finishInBrowser(t, st, "synthetic-code")
	if got := waitDone(t, st.ID); got.State != "failed" {
		t.Fatalf("refused code state: %s", got.State)
	}
}
