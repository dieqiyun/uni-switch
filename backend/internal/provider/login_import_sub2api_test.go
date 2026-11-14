package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// #1975 (KKKKKKKEM): Sub2API keeps a Claude account's email as Anthropic
// names it, email_address, in credentials (the token it was given, whole)
// and again in extra. The reporter's export, as filed, came in as "the file
// doesn't say which Claude account this is". Its account and organization
// uuids and its scope come in too, so two seats of one email under two
// organizations stay two accounts.
func TestImportSub2APIClaudeAccounts(t *testing.T) {
	claudeHome(t)
	asked := false
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = true
		w.WriteHeader(500)
	}))
	defer fake.Close()
	oldBase := claudeBase
	t.Cleanup(func() { claudeBase = oldBase })
	claudeBase = fake.URL

	// the reporter's file, its tokens put back as tokens
	reported := `{
  "accounts": [
    {
      "name": "my-claude-account",
      "platform": "anthropic",
      "type": "oauth",
      "credentials": {
        "email_address": "user@example.com",
        "access_token": "sk-ant-oat01-reported",
        "refresh_token": "sk-ant-ort01-reported"
      }
    }
  ]
}`
	// Sub2API's own shape: CreateAccountModal's credentials = {...tokenInfo},
	// extra = buildExtraInfo(tokenInfo); two Team seats of one email
	seats := `{"exported_at":"2026-10-12T00:00:00Z","proxies":[],"type":"sub2api-data","version":1,"accounts":[
 {"name":"seat-a","platform":"anthropic","type":"oauth","concurrency":3,"priority":50,
  "credentials":{"access_token":"sk-ant-oat01-a","token_type":"Bearer","expires_in":28800,"expires_at":1760000000,"refresh_token":"sk-ant-ort01-a","scope":"user:inference user:profile user:sessions:claude_code","org_uuid":"org-a","account_uuid":"acct-1","email_address":"team@example.com"},
  "extra":{"org_uuid":"org-a","account_uuid":"acct-1","email_address":"team@example.com"}},
 {"name":"seat-b","platform":"anthropic","type":"oauth",
  "credentials":{"access_token":"sk-ant-oat01-b","token_type":"Bearer","expires_in":28800,"expires_at":1760000000,"refresh_token":"sk-ant-ort01-b"},
  "extra":{"org_uuid":"org-b","account_uuid":"acct-1","email_address":"team@example.com"}}
]}`
	rs, err := ImportLogins(context.Background(), "claude", []string{reported, seats})
	if err != nil {
		t.Fatal(err)
	}
	if got := importStatuses(rs); got != "user@example.com:added,team@example.com:added,team@example.com:added" || asked {
		t.Fatalf("got %s %+v asked Anthropic: %v", got, rs, asked)
	}
	byToken := map[string]savedLogin{}
	for _, l := range readLogins() {
		byToken[loginRefreshToken(l)] = l
	}
	for _, c := range []struct {
		token, email, account, org string
		scopes                     []string
	}{
		{"sk-ant-ort01-reported", "user@example.com", "", "", claudeImportScopes},
		{"sk-ant-ort01-a", "team@example.com", "acct-1", "org-a", []string{"user:inference", "user:profile", "user:sessions:claude_code"}},
		{"sk-ant-ort01-b", "team@example.com", "acct-1", "org-b", claudeImportScopes},
	} {
		l, ok := byToken[c.token]
		if !ok {
			t.Fatalf("%s wasn't kept: %v", c.token, byToken)
		}
		email, org := claudeWho(l.Profile)
		id, _ := claudeSessionIdentity(l.Profile)
		cr, _ := parseClaudeCredentials(l.Auth)
		if email != c.email || org != c.org || id.AccountID != c.account || !slices.Equal(cr.OAuth.Scopes, c.scopes) {
			t.Fatalf("%s kept as %s org %q account %q scopes %v; profile %s", c.token, email, org, id.AccountID, cr.OAuth.Scopes, l.Profile)
		}
	}
}
