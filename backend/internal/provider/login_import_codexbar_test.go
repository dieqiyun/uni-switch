package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// codexbarConfig is ~/.codexbar/config.json as codexbar (lizhelang/codexbar
// 9f2e0f53) writes it: JSONEncoder, prettyPrinted and sortedKeys, dates in
// ISO 8601, nil fields left out. Two seats of one email in the OAuth
// provider, the remote connection holding one of them again, an account
// with no refresh token, and an API-compatible provider with a key.
const codexbarConfig = `{
  "active" : {
    "accountId" : "user-a__acct-a",
    "providerId" : "openai-oauth"
  },
  "global" : {
    "defaultModel" : "gpt-5.5",
    "reviewModel" : "gpt-5.5"
  },
  "openAI" : {
    "accountOrder" : [
      "user-a__acct-a",
      "user-b__acct-b"
    ],
    "remoteConnectionAccountID" : "user-a__acct-a",
    "remoteConnectionAccounts" : [
      {
        "accessToken" : "y",
        "email" : "team@example.com",
        "id" : "user-a__acct-a",
        "idToken" : "x",
        "kind" : "oauth_tokens",
        "label" : "team@example.com",
        "openAIAccountId" : "acct-a",
        "refreshToken" : "r-a"
      }
    ]
  },
  "providers" : [
    {
      "accounts" : [
        {
          "accessToken" : "y",
          "addedAt" : "2026-09-30T08:12:44Z",
          "email" : "team@example.com",
          "expiresAt" : "2026-10-10T08:12:44Z",
          "id" : "user-a__acct-a",
          "idToken" : "x",
          "kind" : "oauth_tokens",
          "label" : "team@example.com",
          "lastRefresh" : "2026-10-09T08:12:44Z",
          "oauthClientID" : "app_EMoamEEZ73f0CkXaXp7hrann",
          "openAIAccountId" : "acct-a",
          "planType" : "team",
          "refreshToken" : "r-a"
        },
        {
          "accessToken" : "y",
          "email" : "team@example.com",
          "id" : "user-b__acct-b",
          "idToken" : "x",
          "kind" : "oauth_tokens",
          "label" : "team@example.com",
          "openAIAccountId" : "acct-b",
          "refreshToken" : "r-b"
        },
        {
          "accessToken" : "y",
          "email" : "session@example.com",
          "id" : "user-c__acct-c",
          "kind" : "oauth_tokens",
          "label" : "session@example.com",
          "openAIAccountId" : "acct-c"
        }
      ],
      "activeAccountId" : "user-a__acct-a",
      "cachedModelCatalog" : [

      ],
      "enabled" : true,
      "id" : "openai-oauth",
      "kind" : "openai_oauth",
      "label" : "OpenAI",
      "pinnedModelIDs" : [

      ],
      "wireAPI" : "responses"
    },
    {
      "accounts" : [
        {
          "apiKey" : "sk-deepseek",
          "id" : "6F1C2B9E-0D3A-4E57-9B1F-2A7C8E4D5F60",
          "kind" : "api_key",
          "label" : "Default"
        }
      ],
      "baseURL" : "https://api.deepseek.com/v1",
      "cachedModelCatalog" : [

      ],
      "enabled" : true,
      "id" : "deepseek",
      "kind" : "openai_compatible",
      "label" : "DeepSeek",
      "pinnedModelIDs" : [

      ],
      "presetID" : "deepseek",
      "wireAPI" : "chat"
    }
  ],
  "version" : 1
}`

// 碳碳双键 on Discord: several Codex accounts switched with codexbar on the
// old computer, only the one Codex was signed in to on the new one. A
// backup carries no sign-in, so codexbar's own file is what brings them:
// each ChatGPT account in it is imported, its seat kept apart by
// openAIAccountId, and nothing else in the file taken for an account.
func TestImportCodexbarConfig(t *testing.T) {
	claudeHome(t)
	var mu sync.Mutex
	asked := map[string]int{}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		rt := body["refresh_token"]
		mu.Lock()
		asked[rt]++
		mu.Unlock()
		if rt != "r-a" && rt != "r-b" {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant"})
			return
		}
		// an id token that doesn't name the workspace: the file's
		// openAIAccountId is what tells the two seats apart
		json.NewEncoder(w).Encode(map[string]any{
			"id_token":      fakeJWT(map[string]any{"email": "team@example.com", "https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "team"}}),
			"access_token":  fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix())}),
			"refresh_token": "n" + rt,
		})
	}))
	defer fake.Close()
	t.Cleanup(CodexTokenURLForTest(fake.URL))

	rs, err := ImportLogins(context.Background(), "codex", []string{codexbarConfig})
	if err != nil {
		t.Fatal(err)
	}
	want := "team@example.com · Team:added,team@example.com · Team:added,session@example.com:failed"
	if got := importStatuses(rs); got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
	if !strings.Contains(rs[2].Error, "only an access token") {
		t.Errorf("no refresh token: %q", rs[2].Error)
	}
	if asked["r-a"] != 1 || asked["r-b"] != 1 || len(asked) != 2 {
		t.Errorf("asked %v", asked)
	}
	loginsMu.Lock()
	ls := readLogins()
	loginsMu.Unlock()
	ws := map[string]bool{}
	for _, l := range ls {
		if e, w := codexWho(l.Auth); e == "team@example.com" {
			ws[w] = true
		}
	}
	if !ws["acct-a"] || !ws["acct-b"] || len(ls) != 2 {
		t.Errorf("seats kept: %v (%d logins)", ws, len(ls))
	}

	// for Claude, nothing in it is Claude's
	rs, _ = ImportLogins(context.Background(), "claude", []string{codexbarConfig})
	for _, r := range rs {
		if r.Status != "failed" || !strings.Contains(r.Error, "ChatGPT account") {
			t.Errorf("claude: %+v", r)
		}
	}
}
