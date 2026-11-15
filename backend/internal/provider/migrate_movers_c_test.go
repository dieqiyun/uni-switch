package provider

import (
	"encoding/json"
	"fmt"
	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/update"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// saveLogins writes logins.json as ls.
func saveLogins(t *testing.T, ls ...savedLogin) {
	t.Helper()
	loginsMu.Lock()
	defer loginsMu.Unlock()
	if err := writeLogins(ls); err != nil {
		t.Fatal(err)
	}
}

// loginOf is user's entry for agent in ls.
func loginOf(t *testing.T, ls []savedLogin, agent, user string) savedLogin {
	t.Helper()
	for _, l := range ls {
		if l.Agent == agent && l.User == user {
			return l
		}
	}
	t.Fatalf("no %s login for %q in %+v", agent, user, ls)
	return savedLogin{}
}

func movingOf(t *testing.T, id string) map[string]Moving {
	t.Helper()
	ms, err := movers[id].out()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]Moving{}
	for _, m := range ms {
		out[m.User] = m
	}
	return out
}

// Kiro's own sign-in goes as a marker, an account uni-switch signed in whole,
// the provider's key as an API key account (off the provider while the
// plugin has it); back, a renewed sign-in is written into its home, one
// signed in since gets a home, and the key goes back onto the provider.
func TestKiroMover(t *testing.T) {
	kiroSandbox(t)
	writeFile(t, filepath.Join(kiroIDEDir(), "kiro-auth-token.json"), map[string]any{"accessToken": "ide-a", "refreshToken": "ide-r"})
	home, err := newKiroHome()
	if err != nil {
		t.Fatal(err)
	}
	exp := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	writeFile(t, filepath.Join(home, kiroTokenFile), kiroSaved{AccessToken: "b-a", RefreshToken: "b-r", ExpiresAt: exp.Format(time.RFC3339Nano),
		AuthMethod: "IdC", Provider: "Enterprise", Region: "eu-west-1", ProfileArn: "arn:p", ClientID: "cid", ClientSecret: "cs"})
	saveLogins(t, savedLogin{Agent: "kiro", User: "b@x", Home: home, On: true, Plan: "Pro"})
	if err := setKiroKey("ksk_1"); err != nil {
		t.Fatal(err)
	}

	ms := movingOf(t, "kiro")
	if k := ms["Kiro API key"]; !k.First || k.Auth["type"] != "api" || k.Auth["key"] != "ksk_1" || k.Auth["accountId"] != "Kiro API key" {
		t.Fatalf("the key: %+v", k)
	}
	// retired and not moved, the own sign-in still goes, as a marker
	own := ms["Kiro account"]
	if !own.Own || own.First || own.Auth["source"] != "kiro" || own.Auth["access"] != "" {
		t.Fatalf("Kiro's own: %+v", own)
	}
	// with the key, the built-in used nothing else: b goes along off
	b := ms["b@x"]
	if b.Own || b.First || b.On || b.Auth["access"] != "b-a" || b.Auth["refresh"] != "b-r" || b.Auth["expires"] != exp.UnixMilli() ||
		b.Auth["method"] != "idc" || b.Auth["loginProvider"] != "Enterprise" || b.Auth["region"] != "eu-west-1" ||
		b.Auth["profileArn"] != "arn:p" || b.Auth["clientId"] != "cid" || b.Auth["clientSecret"] != "cs" || b.Auth["plan"] != "Pro" {
		t.Fatalf("b@x: %+v", b.Auth)
	}

	kept, err := movers["kiro"].take()
	if err != nil || kiroKey() != "" {
		t.Fatalf("taking the key: %v, %q left", err, kiroKey())
	}

	// the plugin's, as JSON gives them back
	var renewed map[string]any
	_ = json.Unmarshal([]byte(jsonText(b.Auth)), &renewed)
	renewed["access"], renewed["refresh"] = "b-a2", "b-r2"
	ls, user, err := movers["kiro"].back(readLogins(), "b@x", renewed)
	if err != nil || user != "b@x" || loginOf(t, ls, "kiro", "b@x").Home != home {
		t.Fatalf("back: %v %q %+v", err, user, ls)
	}
	n := len(ls)
	c, ok := readKiroFile(filepath.Join(home, kiroTokenFile))
	if !ok || c.access != "b-a2" || c.refresh != "b-r2" || !c.expires.Equal(exp) || c.method != "idc" || c.clientID != "cid" || c.profile != "arn:p" {
		t.Fatalf("b@x's home after going back: %+v", c)
	}

	// one signed in through the plugin since: a home of its own
	ls, user, err = movers["kiro"].back(ls, "", map[string]any{"type": "oauth", "access": "n-a", "refresh": "n-r", "expires": float64(exp.UnixMilli()),
		"method": "social", "loginProvider": "Google", "accountId": "n@x"})
	nx := loginOf(t, ls, "kiro", "n@x")
	if err != nil || user != "n@x" || len(ls) != n+1 || nx.Home == "" || nx.Home == home {
		t.Fatalf("a new account back: %v %q %+v", err, user, ls)
	}
	if c, ok := readKiroFile(filepath.Join(nx.Home, kiroTokenFile)); !ok || c.access != "n-a" || !c.social {
		t.Fatalf("n@x's home: %+v", c)
	}
	// Kiro's own: nothing to write
	if ls2, user, err := movers["kiro"].back(ls, "Kiro account", own.Auth); err != nil || user != "Kiro account" || len(ls2) != n+1 {
		t.Fatalf("Kiro's own back: %v %q", err, user)
	}
	// the key: back onto the provider once logins.json is let go, saving
	// the provider syncing the agents, which read the accounts (back runs
	// holding loginsMu: saving it there hung the move back)
	catalog.Changed = func() { loginsMu.Lock(); loginsMu.Unlock() }
	t.Cleanup(func() { catalog.Changed = nil })
	key := map[string]any{"type": "api", "key": "ksk_1"}
	done := make(chan error, 1)
	go func() {
		loginsMu.Lock()
		_, _, err := movers["kiro"].back(ls, "", key)
		loginsMu.Unlock()
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil || kiroKey() != "" {
			t.Fatalf("the key back: %v, %q on the provider before settling", err, kiroKey())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the key's back hung saving the provider under loginsMu")
	}
	if err := movers["kiro"].settle(map[string]map[string]any{"k": key}); err != nil || kiroKey() != "ksk_1" {
		t.Fatalf("the key settled: %v %q", err, kiroKey())
	}
	_ = setKiroKey("")
	if err := movers["kiro"].give(kept); err != nil || kiroKey() != "ksk_1" {
		t.Fatalf("giving the key back: %v %q", err, kiroKey())
	}
}

// WorkBuddy desktop's sign-in goes as a marker the plugin reads the app's
// file by, never as tokens; one uni-switch signed in goes whole, and comes back
// with the plugin's renewed tokens.
func TestWorkBuddyMover(t *testing.T) {
	home := signIn(t)
	wbTokens.Lock()
	wbTokens.m = map[string]wbCreds{}
	wbTokens.Unlock()
	wbWriteOwn(t, home, "own-access")
	two := wbCreds{UID: "u2", Access: "a2", Refresh: "r2", ExpiresAt: 1_900_000_000_000, RefreshExpiresAt: 1_950_000_000_000, Domain: "www.codebuddy.cn", TokenType: "Bearer"}
	// the desktop's row is the one the built-in wrote while it ran: retired,
	// nothing writes it anew
	saveLogins(t, savedLogin{Agent: "workbuddy", User: "Me", On: true},
		savedLogin{Agent: "workbuddy", User: "Two", On: true, Auth: json.RawMessage(jsonText(two))})

	ms := movingOf(t, "workbuddy")
	own := ms["Me"]
	if !own.Own || own.Auth["source"] != "desktop" || own.Auth["access"] != "" || own.Auth["refresh"] != "" || own.Auth["uid"] != "u1" {
		t.Fatalf("desktop's: %+v", own)
	}
	b := ms["Two"]
	if b.Own || b.Auth["access"] != "a2" || b.Auth["refresh"] != "r2" || b.Auth["expires"] != two.ExpiresAt ||
		b.Auth["refreshExpiresAt"] != two.RefreshExpiresAt || b.Auth["uid"] != "u2" || b.Auth["domain"] != two.Domain || b.Auth["tokenType"] != "Bearer" {
		t.Fatalf("Two: %+v", b.Auth)
	}
	if len(movingOf(t, WorkBuddyAIID)) != 0 {
		t.Fatal("WorkBuddy AI has no account here")
	}

	var renewed map[string]any
	_ = json.Unmarshal([]byte(jsonText(b.Auth)), &renewed)
	renewed["access"], renewed["refresh"], renewed["expires"] = "a3", "r3", float64(1_910_000_000_000)
	ls, user, err := movers["workbuddy"].back(readLogins(), "Two", renewed)
	if err != nil || user != "Two" {
		t.Fatalf("back: %v %q %+v", err, user, ls)
	}
	got, ok := wbSavedCreds(loginOf(t, ls, "workbuddy", "Two"))
	want := two
	want.Access, want.Refresh, want.ExpiresAt = "a3", "r3", 1_910_000_000_000
	if !ok || got != want {
		t.Fatalf("Two back: %+v, want %+v", got, want)
	}
	if ls2, user, err := movers["workbuddy"].back(ls, "Me", own.Auth); err != nil || user != "Me" || len(ls2) != len(ls) {
		t.Fatalf("desktop's back: %v %q %+v", err, user, ls2)
	}
}

// Two WorkBuddy accounts of one nickname are two accounts, told apart by
// uid: the desktop's goes beside one uni-switch signed in under its name, and
// neither reads as renewed by the other (wakkana on Discord). The same uid
// under the desktop's name is the desktop's account, moved once.
func TestWorkBuddyMoverSameNameOtherUID(t *testing.T) {
	home := signIn(t)
	wbWriteOwn(t, home, "own-access") // "Me", u1
	other := wbCreds{UID: "u9", Access: "a9", Refresh: "r9", ExpiresAt: 1_900_000_000_000}
	saveLogins(t, savedLogin{Agent: "workbuddy", User: "Me", On: true, Auth: json.RawMessage(jsonText(other))})
	ms, err := movers["workbuddy"].out()
	if err != nil {
		t.Fatal(err)
	}
	uids := map[string]bool{}
	var moved []movedAccount
	for i, m := range ms {
		uids[str(m.Auth["uid"])+map[bool]string{true: " desktop"}[m.Own]] = true
		moved = append(moved, movedAccount{Key: fmt.Sprint("k", i), User: m.User, Own: m.Own, ID: m.ID, Sent: signinHash(m.Auth)})
	}
	if len(ms) != 2 || !uids["u1 desktop"] || !uids["u9"] {
		t.Fatalf("moving %+v", ms)
	}
	if r := renewedSince(movers["workbuddy"], moved); len(r) != 0 {
		t.Fatalf("read as renewed: %+v", r)
	}

	same := other
	same.UID = "u1"
	saveLogins(t, savedLogin{Agent: "workbuddy", User: "Me", On: true, Auth: json.RawMessage(jsonText(same))})
	if ms := movingOf(t, "workbuddy"); len(ms) != 1 || ms["Me"].Auth["access"] != "a9" {
		t.Fatalf("the same uid: %+v", ms)
	}
}

// wbWriteOwn signs WorkBuddy desktop in under home, as its app does.
func wbWriteOwn(t *testing.T, home string, access any) {
	t.Helper()
	writeFile(t, wbAuthFile(home), map[string]any{
		"account": map[string]any{"uid": "u1", "nickname": "Me"},
		"auth": map[string]any{"accessToken": access, "refreshToken": "r",
			"expiresAt": time.Now().Add(24 * time.Hour).UnixMilli(), "domain": "www.codebuddy.cn"},
	})
}

// wbAuthFile is where WorkBuddy desktop keeps its sign-in under home.
func wbAuthFile(home string) string {
	base := filepath.Join(home, ".local", "share")
	switch runtime.GOOS {
	case "darwin":
		base = filepath.Join(home, "Library", "Application Support")
	case "windows":
		base = filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(base, "CodeBuddyExtension", "Data", "Public", "auth", "workbuddy-desktop.info")
}

// ZCode's accounts go as the plugin's sign-in keeps them and come back as
// they were; ZCode's own goes marked as ZCode's, for the plugin to read
// ZCode's sign-in anew, its session alone (the Start Plan) too.
func TestZCodeMover(t *testing.T) {
	home := signIn(t)
	t.Setenv("ZCODE_CREDENTIAL_SECRET", "test-secret")
	creds := filepath.Join(home, ".zcode", "v2", "credentials.json")
	writeFile(t, creds, map[string]any{
		"oauth:zai:user_info": zcodeEncrypt(t, `{"user_id":"u1","email":"own@example.com"}`),
		"account-provider:coding-plan:account:zai-individual-coding-plan:account:abc:api-key": zcodeEncrypt(t, "own.secret"),
	})
	team := zcodeKey{Base: ZCodeBigModelBase, Token: "biz", Org: "o1", Project: "p1", Key: "team.key"}
	saveLogins(t, savedLogin{Agent: "zcode", User: "team@x", Plan: "Team", On: true, Auth: json.RawMessage(jsonText(team))})

	ms := movingOf(t, "zcode")
	own := ms["own@example.com"]
	var st map[string]any
	if !own.Own || own.Auth["access"] != "own.secret" || json.Unmarshal([]byte(str(own.Auth["refresh"])), &st) != nil ||
		st["site"] != "zai" || st["key"] != "own.secret" || st["device"] != zcodeDeviceMid() || st["base"] != ZCodeZaiBase || st["source"] != "zcode" {
		t.Fatalf("ZCode's own: %+v %+v", own, st)
	}
	// marked as ZCode's, it comes back as ZCode's own, with nothing saved
	var ownBack map[string]any
	_ = json.Unmarshal([]byte(jsonText(own.Auth)), &ownBack)
	before := len(readLogins())
	if ls, user, err := movers["zcode"].back(readLogins(), "own@example.com", ownBack); err != nil || len(ls) != before || user != "own@example.com" {
		t.Fatalf("ZCode's own back: %v %q %+v", err, user, ls)
	}
	tm := ms["team@x"]
	st = nil
	if json.Unmarshal([]byte(str(tm.Auth["refresh"])), &st) != nil || st["source"] != nil || st["site"] != "bigmodel" || st["org"] != "o1" ||
		st["project"] != "p1" || st["token"] != "biz" || st["plan"] != "Team" || tm.Auth["expires"] != int64(0) {
		t.Fatalf("team@x: %+v %+v", tm.Auth, st)
	}

	var back map[string]any
	_ = json.Unmarshal([]byte(jsonText(tm.Auth)), &back)
	ls, user, err := movers["zcode"].back(readLogins(), "team@x", back)
	if err != nil || user != "team@x" {
		t.Fatalf("back: %v %q %+v", err, user, ls)
	}
	if tl := loginOf(t, ls, "zcode", "team@x"); tl.Plan != "Team" {
		t.Fatalf("team@x back: %+v", tl)
	} else if k, ok := zcodeSaved(tl); !ok || k != team {
		t.Fatalf("team@x back: %+v", k)
	}
	// a key saved in the plugin since
	ls, user, err = movers["zcode"].back(ls, "", map[string]any{"type": "api", "key": "k.2", "metadata": map[string]any{"site": "bigmodel"}})
	if k, _ := zcodeSaved(loginOf(t, ls, "zcode", "ZCode")); err != nil || user != "ZCode" || k.Key != "k.2" || k.Base != ZCodeBigModelBase {
		t.Fatalf("a key back: %v %q %+v", err, user, k)
	}

	// ZCode's session alone
	writeFile(t, creds, map[string]any{
		"oauth:zai:user_info": zcodeEncrypt(t, `{"user_id":"u1","email":"own@example.com"}`),
		"zcodejwttoken":       zcodeEncrypt(t, zcodeTestJWT(time.Now().Add(time.Hour))),
	})
	if ms := movingOf(t, "zcode"); !ms["own@example.com"].Own || ms["own@example.com"].Auth["expires"] != int64(0) ||
		!strings.Contains(str(ms["own@example.com"].Auth["refresh"]), `"source":"zcode"`) {
		t.Fatalf("ZCode's session alone: %+v", ms["own@example.com"])
	}
	if update.Newer("0.1.2", movers["zcode"].min) {
		t.Fatalf("the move installs zcode-auth %q, which doesn't read ZCode's own", movers["zcode"].min)
	}
	os.Remove(creds)
	if ms := movingOf(t, "zcode"); len(ms) != 1 {
		t.Fatalf("with ZCode signed out: %+v", ms)
	}
}
