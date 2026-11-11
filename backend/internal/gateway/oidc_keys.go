package gateway

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/oidc"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// OIDCKeysPath trades an OIDC token for a short-lived gateway key (#4531):
// a GitHub Actions job asks GitHub for a token with the rule's audience
// and POSTs it here as Authorization: Bearer <token>, with no gateway
// key, and gets a key issued under the rule's gateway key (package oidc).
// The body is optional: {"ttl": "30m", "models": […]}, each no more than
// the rule and its key allow.
//
// It is answered from another machine only while the gateway is shared,
// past the allow list, as every route is (lanGuard); a token that isn't
// let in counts against the failed authentications another machine may
// make (tooManyAuthFails), as a wrong key does.
const OIDCKeysPath = KeysPath + "/oidc"

// oidcExchange: r is a token's trade, which carries a token, not a key.
func oidcExchange(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == OIDCKeysPath && r.URL.RawPath == "" && !managedKey(r)
}

func (s *Server) oidcIssueKey(w http.ResponseWriter, r *http.Request) {
	token := ""
	if a := r.Header.Get("Authorization"); len(a) > 7 && strings.EqualFold(a[:7], "Bearer ") {
		token = strings.TrimSpace(a[7:])
	}
	refuse := func(why string) {
		if !local(r) {
			if over, retry := tooManyAuthFails(); over {
				w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
				writeError(w, provider.Chat, http.StatusTooManyRequests, "too many failed authentications; slow down")
				return
			}
		}
		log.Printf("refused %s %s from %s: %s", r.Method, r.URL.Path, r.RemoteAddr, why)
		writeError(w, provider.Chat, http.StatusUnauthorized, why)
	}
	if token == "" {
		refuse("send the OIDC token as Authorization: Bearer <token>")
		return
	}
	rule, claims, err := oidc.Match(r.Context(), token)
	if err != nil {
		refuse(err.Error())
		return
	}
	var in struct {
		TTL    json.RawMessage `json:"ttl"`
		Models []string        `json:"models"`
	}
	if b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20)); len(strings.TrimSpace(string(b))) > 0 {
		if err := json.Unmarshal(b, &in); err != nil {
			writeError(w, provider.Chat, http.StatusBadRequest, "the body is empty or a JSON object: {\"ttl\": \"30m\", \"models\": […]}")
			return
		}
	}
	ttl := rule.Lifetime()
	if len(in.TTL) > 0 {
		asked, err := parseTTL(in.TTL)
		if err != nil {
			writeError(w, provider.Chat, http.StatusBadRequest, err.Error())
			return
		}
		ttl = min(asked, ttl) // never longer than the rule says
	}
	k, err := access.IssueKey(access.Issue{Name: oidcKeyName(claims), TTL: ttl, Models: in.Models, Issuer: rule.Key})
	if err != nil {
		writeError(w, provider.Chat, http.StatusBadRequest, err.Error())
		return
	}
	log.Printf("issued gateway key %s (%s) for an OIDC token by rule %s", k.ID, k.Name, rule.ID)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, k)
}

// oidcKeyName names a key by what asked for it: a GitHub job's
// repository, ref and run, another issuer's subject.
func oidcKeyName(c oidc.Claims) string {
	name := c.Str("sub")
	if repo := c.Str("repository"); repo != "" {
		name = repo
		if ref := c.Str("ref"); ref != "" {
			name += "@" + strings.TrimPrefix(strings.TrimPrefix(ref, "refs/heads/"), "refs/")
		}
		if run := c.Str("run_id"); run != "" {
			name += " (run " + run + ")"
		}
	}
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, name)
	if utf8.RuneCountInString(name) > 120 {
		name = string([]rune(name)[:119]) + "…"
	}
	if strings.TrimSpace(name) == "" {
		name = "OIDC"
	}
	return name
}
