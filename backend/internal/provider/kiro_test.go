package provider

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// kiroSandbox points Kiro's sign-ins at an empty home of the test's own,
// with kiro-cli nowhere and Kiro never asked who the account is.
func kiroSandbox(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	db, ide, exe, ask := kiroCLIDB, kiroIDEDir, KiroExecutable, askKiroIdentity
	kiroCLIDB = func() string { return filepath.Join(home, "kiro-cli", "data.sqlite3") }
	kiroIDEDir = func() string { return filepath.Join(home, ".aws", "sso", "cache") }
	KiroExecutable = func() string { return "" }
	askKiroIdentity = func(string, string) (string, string) { return "", "" }
	kiroForget()
	t.Cleanup(func() {
		kiroCLIDB, kiroIDEDir, KiroExecutable, askKiroIdentity = db, ide, exe, ask
		kiroForget()
	})
	return home
}

// kiroForget drops what was known of Kiro's accounts.
func kiroForget() {
	kiroAuthCache.Lock()
	kiroAuthCache.m = nil
	kiroAuthCache.Unlock()
	kiroStatus.Lock()
	kiroStatus.m = nil
	kiroStatus.Unlock()
	kiroAdopt.Lock()
	kiroAdopt.tried = time.Time{}
	kiroAdopt.Unlock()
}

// writeKiroCLI makes a kiro-cli database holding rows of auth_kv.
func writeKiroCLI(t *testing.T, rows map[string]any) {
	t.Helper()
	path := kiroCLIDB()
	os.MkdirAll(filepath.Dir(path), 0o755)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS auth_kv (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatal(err)
	}
	for k, v := range rows {
		b, _ := json.Marshal(v)
		if _, err := db.Exec(`INSERT OR REPLACE INTO auth_kv (key, value) VALUES (?, ?)`, k, string(b)); err != nil {
			t.Fatal(err)
		}
	}
}

func readKiroRow(t *testing.T, key string) map[string]any {
	t.Helper()
	db, err := sql.Open("sqlite", kiroCLIDB())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v string
	if err := db.QueryRow(`SELECT value FROM auth_kv WHERE key = ?`, key).Scan(&v); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal([]byte(v), &m)
	return m
}

func TestKiroReadsKiroCLIsSignIn(t *testing.T) {
	kiroSandbox(t)
	if _, ok := readKiroAt("", ""); ok {
		t.Fatal("signed in with nothing there")
	}
	exp := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	// the shape kiro-cli 2.24 keeps a Google sign-in in
	writeKiroCLI(t, map[string]any{"kirocli:social:token": map[string]any{"access_token": "at", "refresh_token": "rt",
		"expires_at": exp, "provider": "google", "profile_arn": "arn:aws:codewhisperer:us-east-1:1:profile/P"}})
	c, ok := readKiroAt("", "")
	if !ok || c.method != "social" || c.access != "at" || c.refresh != "rt" || c.region != "us-east-1" || !c.fresh() || c.dbKey != "kirocli:social:token" {
		t.Fatalf("social = %+v %v", c, ok)
	}
	a, err := KiroAuthOf(context.Background(), "", "", false)
	if err != nil || a.Token != "at" || a.Profile != "arn:aws:codewhisperer:us-east-1:1:profile/P" || a.Region != "us-east-1" || a.TokenType != "" {
		t.Fatalf("auth = %+v %v", a, err)
	}
	// a key saved on the provider comes before any sign-in
	c, _ = readKiroAt("ksk_x", "")
	if c.method != "apikey" || c.access != "ksk_x" {
		t.Fatalf("key = %+v", c)
	}
}

func TestKiroReadsAnAWSSignInAndItsClient(t *testing.T) {
	kiroSandbox(t)
	writeKiroCLI(t, map[string]any{
		"kirocli:odic:token":               map[string]any{"access_token": "at", "refresh_token": "rt", "expires_at": "2020-01-01T00:00:00Z", "region": "eu-west-1"},
		"kirocli:odic:device-registration": map[string]any{"client_id": "cid", "client_secret": "sec"},
	})
	c, ok := readKiroAt("", "")
	if !ok || c.method != "idc" || c.clientID != "cid" || c.clientSecret != "sec" || c.region != "eu-west-1" || c.fresh() {
		t.Fatalf("idc = %+v", c)
	}
	if r := kiroRegion("", c.region); r != "eu-central-1" {
		t.Fatalf("region = %q", r)
	}
}

func TestKiroReadsTheIDEsSignIn(t *testing.T) {
	kiroSandbox(t)
	dir := kiroIDEDir()
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "kiro-auth-token.json"), []byte(`{"accessToken":"at","refreshToken":"rt","expiresAt":"2099-01-01T00:00:00Z","clientIdHash":"h","region":"us-east-1"}`), 0o600)
	os.WriteFile(filepath.Join(dir, "h.json"), []byte(`{"clientId":"cid","clientSecret":"sec"}`), 0o600)
	c, ok := readKiroAt("", "")
	if !ok || c.method != "idc" || c.clientID != "cid" || c.idePath == "" {
		t.Fatalf("ide = %+v", c)
	}
	os.WriteFile(filepath.Join(dir, "kiro-auth-token.json"), []byte(`{"accessToken":"at","refreshToken":"rt","expiresAt":"2099-01-01T00:00:00Z","authMethod":"social","provider":"Github"}`), 0o600)
	if c, _ := readKiroAt("", ""); c.method != "social" {
		t.Fatalf("ide social = %+v", c)
	}
}

// An expired Google sign-in is refreshed and the new token written back
// into kiro-cli's database, so the CLI goes on with it.
func TestKiroRefreshesAndWritesBack(t *testing.T) {
	kiroSandbox(t)
	writeKiroCLI(t, map[string]any{"kirocli:social:token": map[string]any{"access_token": "old", "refresh_token": "rt",
		"expires_at": "2020-01-01T00:00:00Z", "provider": "google", "profile_arn": "arn:aws:codewhisperer:us-east-1:1:profile/P"}})
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"accessToken":"new","refreshToken":"rt2","expiresIn":3600}`))
	}))
	defer srv.Close()
	was := kiroRefreshURL
	kiroRefreshURL = func(string) string { return srv.URL }
	defer func() { kiroRefreshURL = was }()

	a, err := KiroAuthOf(context.Background(), "", "", false)
	if err != nil || a.Token != "new" {
		t.Fatalf("auth = %+v %v", a, err)
	}
	if got["refreshToken"] != "rt" {
		t.Fatalf("refreshed with %v", got)
	}
	row := readKiroRow(t, "kirocli:social:token")
	if row["access_token"] != "new" || row["refresh_token"] != "rt2" || row["provider"] != "google" {
		t.Fatalf("row = %v", row)
	}
	if exp, _ := time.Parse(time.RFC3339Nano, row["expires_at"].(string)); time.Until(exp) < 50*time.Minute {
		t.Fatalf("expires_at = %v", row["expires_at"])
	}
	// turned down later, it is refreshed again even though it hasn't expired
	a, err = KiroAuthOf(context.Background(), "", "", true)
	if err != nil || a.Token != "new" || got["refreshToken"] != "rt2" {
		t.Fatalf("stale = %+v %v %v", a, err, got)
	}
}

func TestKiroExpiredWithNoWayToRefresh(t *testing.T) {
	kiroSandbox(t)
	writeKiroCLI(t, map[string]any{"kirocli:social:token": map[string]any{"access_token": "old", "expires_at": "2020-01-01T00:00:00Z"}})
	if _, err := KiroAuthOf(context.Background(), "", "", false); err == nil || !strings.Contains(err.Error(), "kiro-cli login") {
		t.Fatalf("err = %v", err)
	}
}

func TestKiroRegion(t *testing.T) {
	for _, c := range []struct{ profile, signedIn, want string }{
		{"arn:aws:codewhisperer:eu-central-1:1:profile/P", "us-east-1", "eu-central-1"},
		{"arn:aws:codewhisperer:us-east-1:1:profile/P", "", "us-east-1"},
		{"", "eu-west-2", "eu-central-1"},
		{"", "ap-southeast-1", "us-east-1"},
	} {
		if got := kiroRegion(c.profile, c.signedIn); got != c.want {
			t.Errorf("%q %q: %q", c.profile, c.signedIn, got)
		}
	}
}

func TestKiroLimits(t *testing.T) {
	// the shape Get-Usage-Limits answered with, on a free account
	var l kiroLimits
	json.Unmarshal([]byte(`{"subscriptionInfo":{"subscriptionTitle":"KIRO FREE"},"userInfo":{"email":"me@example.com"},
		"usageBreakdownList":[{"displayName":"Credit","displayNamePlural":"Credits","currentUsageWithPrecision":12.5,"usageLimitWithPrecision":50,"nextDateReset":1790000000,
		"freeTrialInfo":{"freeTrialStatus":"ACTIVE","currentUsageWithPrecision":100,"usageLimitWithPrecision":500,"freeTrialExpiry":1791000000}}]}`), &l)
	if l.plan() != "Kiro Free" || l.UserInfo.Email != "me@example.com" {
		t.Fatalf("plan = %q", l.plan())
	}
}

// A key saved on Kiro is kept with it, where another account's is not, and
// it is how a Kiro that isn't signed in is added.
func TestSaveKeepsKirosKey(t *testing.T) {
	kiroSandbox(t)
	if err := Save(Provider{ID: "kiro", Name: "kiro", Key: "ksk_1", Chat: "http://ignored"}); err != nil {
		t.Fatal(err)
	}
	if got := kiroKey(); got != "ksk_1" {
		t.Fatalf("key = %q", got)
	}
	for _, p := range load().Providers {
		if p.ID == "kiro" && (p.Chat != "" || p.Name != "") {
			t.Fatalf("kept more than the key: %+v", p)
		}
	}
	// once saved, Kiro's record stays the account's: it never turns into
	// an HTTP provider that would hide the subscription
	if err := Save(Provider{ID: "kiro", Name: "kiro", Key: "ksk_2", Chat: "http://x"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range load().Providers {
		if p.ID == "kiro" && (p.Chat != "" || p.Key != "ksk_2") {
			t.Fatalf("saved %+v", p)
		}
	}
	// and a fresh "kiro" without a key is still refused as a custom name
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := Save(Provider{ID: "kiro", Name: "kiro", Key: "", Chat: "http://x"}); err == nil {
		t.Fatal("a custom provider took Kiro's id")
	}
}
