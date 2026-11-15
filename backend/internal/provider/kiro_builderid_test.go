package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// fakeKiroManagement answers Kiro's management API as it answers a Builder
// ID sign-in: no profiles to list (a 403).
func fakeKiroManagement(t *testing.T) *[]string {
	t.Helper()
	var mu sync.Mutex
	asked := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/List-Available-Profiles":
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"message":"AWS Builder ID is not supported for this operation."}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	was := kiroManagementURL
	kiroManagementURL = func(string) string { return srv.URL + "/" }
	t.Cleanup(func() { kiroManagementURL = was })
	return &asked
}

// A Builder ID sign-in (kiro-cli's here) names no profile and Kiro lists
// none for it (#422); it is asked with Builder ID's service profile, as
// kiro-cli and the IDE do — which is how the move learns whose it is.
func TestKiroBuilderIDTakesItsServiceProfile(t *testing.T) {
	kiroSandbox(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	asked := fakeKiroManagement(t)
	writeKiroCLI(t, map[string]any{
		"kirocli:odic:token": map[string]any{"access_token": "at", "refresh_token": "rt", "expires_at": "2099-01-01T00:00:00Z",
			"region": "us-east-1", "start_url": kiroBuilderIDStart, "oauth_flow": "DeviceCode"},
		"kirocli:odic:device-registration": map[string]any{"client_id": "cid", "client_secret": "sec"},
	})
	a, err := KiroAuthOf(context.Background(), "", "", false)
	if err != nil || a.Profile != kiroBuilderIDProfile || a.Region != "us-east-1" {
		t.Fatalf("auth = %+v %v", a, err)
	}
	for _, a := range *asked {
		if a == "/List-Available-Profiles" {
			t.Fatalf("asked for a Builder ID sign-in's profiles: %v", *asked)
		}
	}
}

// An Identity Center sign-in still has its profile listed; one that turns
// out to be Builder ID's (the IDE's file not saying) takes Builder ID's
// profile when Kiro says it has none to list.
func TestKiroBuilderIDFoundByKirosAnswer(t *testing.T) {
	kiroSandbox(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	asked := fakeKiroManagement(t)
	dir := kiroIDEDir()
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "kiro-auth-token.json"), []byte(`{"accessToken":"at","refreshToken":"rt","expiresAt":"2099-01-01T00:00:00Z",
		"authMethod":"IdC","region":"us-east-1","clientIdHash":"h"}`), 0o600)
	os.WriteFile(filepath.Join(dir, "h.json"), []byte(`{"clientId":"cid","clientSecret":"sec"}`), 0o600)
	a, err := KiroAuthOf(context.Background(), "", "", false)
	if err != nil || a.Profile != kiroBuilderIDProfile {
		t.Fatalf("auth = %+v %v", a, err)
	}
	if len(*asked) == 0 || (*asked)[0] != "/List-Available-Profiles" {
		t.Fatalf("asked %v", *asked)
	}
}
