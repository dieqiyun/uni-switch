package provider

// Local identities for associating historical session metadata. No credential
// refresh, sign-in discovery command or login-store write is needed here.
import (
	"cmp"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/filememo"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// SessionIdentity is an account's public identity, without its credentials.
type SessionIdentity struct {
	Agent, AccountID, UserID, OrganizationID, User string
	// OfficialLogin describes this account's recorded login method, not the
	// route or credentials used for any particular historical request.
	OfficialLogin bool
}

// SessionIdentities reads the agent's current identity and remembered identities.
// Callers must match the IDs recorded in a session; presence alone is not evidence
// that the account served that session's requests.
func SessionIdentities(codexDir string) []SessionIdentity {
	if settings.AccountDiscoveryOff() {
		return nil
	}
	out := currentSessionIdentities(codexDir)
	var saved []SessionIdentity
	for _, l := range readLogins() {
		if id, ok := savedSessionIdentity(l); ok {
			saved = append(saved, id)
		}
	}
	for i, id := range out {
		// the account Codex is signed in to goes by the name it is saved
		// under: a second Team seat of one email is "email · Team ·
		// <workspace>", not the other seat's codexUser name (#1424)
		if id.Agent == "codex" {
			out[i].User = savedCodexName(saved, id)
		}
	}
	for _, id := range saved {
		// The account signed in now is saved too once rememberLogins has
		// seen it. It is listed once, so the usage cache keyed on this list
		// isn't thrown away when that copy is saved.
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// savedCodexName is the name the Codex identity id, read from auth.json
// by codexUser's name, is saved under: that of the saved one of its
// workspace and member whose name is codexUser's, or codexUser's with
// codexName's workspace after it. Not saved yet, it is codexUser's.
func savedCodexName(saved []SessionIdentity, id SessionIdentity) string {
	for _, s := range saved {
		if s.Agent == "codex" && s.AccountID == id.AccountID && s.UserID == id.UserID &&
			(strings.EqualFold(s.User, id.User) || strings.HasPrefix(strings.ToLower(s.User), strings.ToLower(id.User)+" · ")) {
			return s.User
		}
	}
	return id.User
}

func savedSessionIdentity(l savedLogin) (SessionIdentity, bool) {
	switch l.Agent {
	case "codex":
		id, ok := codexSessionIdentity(l.Auth)
		if ok && l.User != "" {
			id.User = l.User // as it is saved (codexName), not codexUser's
		}
		return id, ok
	case "claude":
		return claudeSessionIdentity(l.Profile)
	}
	return SessionIdentity{}, false
}

func codexSessionIdentity(b []byte) (SessionIdentity, bool) {
	var a codexAuth
	if json.Unmarshal(b, &a) != nil || a.AuthMode == "apikey" {
		return SessionIdentity{}, false
	}
	claims := jwtClaims(a.Tokens.IDToken)
	id := cmp.Or(a.Tokens.AccountID, claimString(claims, "https://api.openai.com/auth", "chatgpt_account_id"))
	user := codexUser(claims)
	return SessionIdentity{Agent: "codex", AccountID: id, OfficialLogin: a.AuthMode == "chatgpt",
		UserID: cmp.Or(claimString(claims, "https://api.openai.com/auth", "chatgpt_user_id"), claimString(claims, "https://api.openai.com/auth", "user_id")), User: user}, id != "" && user != ""
}

// CodexSignedInAccount is the ChatGPT account id (chatgpt-account-id)
// Codex's auth.json is signed in to now; "" when it is signed in to none,
// on an API key, or can't be read.
func CodexSignedInAccount() string {
	b, err := readAccountFile(codexAuthPath())
	if err != nil {
		return ""
	}
	id, _ := codexSessionIdentity(b)
	return id.AccountID
}

func claudeSessionIdentity(b []byte) (SessionIdentity, bool) {
	var a struct {
		Account string `json:"accountUuid"`
		Org     string `json:"organizationUuid"`
		Email   string `json:"emailAddress"`
	}
	if json.Unmarshal(b, &a) != nil {
		return SessionIdentity{}, false
	}
	// This profile is read only from Claude's oauthAccount or its saved copy.
	return SessionIdentity{Agent: "claude", AccountID: a.Account, OrganizationID: a.Org, User: a.Email, OfficialLogin: true}, a.Account != "" && a.Email != ""
}

func currentSessionIdentities(codexDir string) []SessionIdentity {
	if settings.AccountDiscoveryOff() {
		return nil
	}
	var out []SessionIdentity
	// Cache only the identity parse, not another copy of the credential blob.
	if b, st, err := readCodexLogin(codexDir, false); err == nil && st.keychain {
		// Codex keeps its sign-in in the keychain item (#2367); one
		// auth.json left beside it isn't the one Codex goes by
		if id, ok := codexSessionIdentity(b); ok {
			out = append(out, id)
		}
	} else {
		current, _ := filememo.Read("session codex identity", filepath.Join(codexDir, "auth.json"), func(b []byte) ([]SessionIdentity, error) {
			if id, ok := codexSessionIdentity(b); ok {
				return []SessionIdentity{id}, nil
			}
			return nil, nil
		})
		out = append(out, current...)
	}
	if acct, ok := claudeProfileAccount(); ok {
		b, _ := json.Marshal(acct)
		if id, ok := claudeSessionIdentity(b); ok {
			out = append(out, id)
		}
	}
	return out
}
