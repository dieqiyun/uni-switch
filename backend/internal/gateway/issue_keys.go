package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// KeysPath issues short-lived gateway keys (xazaj on X: an orchestrator
// gives each sandboxed agent a key of its own that stops working by
// itself). POST issues one, GET lists those the caller issued, DELETE
// KeysPath/{id} revokes one early.
//
// Who may issue: a gateway key the user let issue keys (Key.Issues,
// uni-switch gateway-key issuer <id> on), from wherever it is let in, every
// key it issues bounded by it (access.IssueKey); or, with no key, a
// program on this computer over loopback that carries no browser's marks
// (profileRefused's rule), which may already use every model and could
// run uni-switch gateway-key itself. A page in a browser, another machine
// without such a key, a held or read-only key and an issued key never
// may.
const KeysPath = "/v1/uni-switch/keys"

// issuedKeyBody is a POST's body.
type issuedKeyBody struct {
	Name string `json:"name"`
	// TTL is a Go duration ("1h", "30m") or a number of seconds.
	TTL      json.RawMessage `json:"ttl"`
	Models   []string        `json:"models"`
	Accounts []string        `json:"accounts"`
	Tokens   int64           `json:"tokens"`
	Cost     float64         `json:"cost"`
	ReadOnly bool            `json:"readOnly"`
}

// keyIssuer is the id of the key r issues under ("" for the user, over
// loopback), or why it may not issue, with the status to say it with.
func keyIssuer(r *http.Request) (string, int, string) {
	who := access.Caller(r.Context())
	if who.KeyID != "" {
		if who.Issuer != nil {
			return "", http.StatusForbidden, "this gateway key was issued itself, so it can't issue keys"
		}
		if !who.Issues {
			return "", http.StatusForbidden, "this gateway key can't issue keys: let it with uni-switch gateway-key issuer " + who.KeyID + " on"
		}
		return who.KeyID, 0, ""
	}
	if why := profileRefused(r, "keys", "uni-switch gateway-key issue"); why != "" {
		return "", http.StatusForbidden, why + ", or send a gateway key that may issue keys (uni-switch gateway-key issuer <id> on)"
	}
	return "", 0, ""
}

func (s *Server) issueKey(w http.ResponseWriter, r *http.Request) {
	by, status, why := keyIssuer(r)
	if why != "" {
		log.Printf("refused %s %s: %s", r.Method, r.URL.Path, why)
		writeError(w, provider.Chat, status, why)
		return
	}
	var in issuedKeyBody
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeError(w, provider.Chat, http.StatusBadRequest, "the body is a JSON object: {\"ttl\": \"1h\", \"name\": …, \"models\": […]}")
		return
	}
	ttl, err := parseTTL(in.TTL)
	if err != nil {
		writeError(w, provider.Chat, http.StatusBadRequest, err.Error())
		return
	}
	k, err := access.IssueKey(access.Issue{Name: in.Name, TTL: ttl, Models: in.Models, Accounts: in.Accounts,
		Tokens: in.Tokens, Cost: in.Cost, ReadOnly: in.ReadOnly, Issuer: by})
	if err != nil {
		writeError(w, provider.Chat, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, k)
}

func (s *Server) issuedKeys(w http.ResponseWriter, r *http.Request) {
	by, status, why := keyIssuer(r)
	if why != "" {
		writeError(w, provider.Chat, status, why)
		return
	}
	keys, err := access.IssuedBy(by)
	if err != nil {
		writeError(w, provider.Chat, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		e := map[string]any{"id": k.ID, "name": k.Name, "key": k.Masked, "expires": k.Expires, "expired": !time.Now().Before(k.Expires)}
		if len(k.Models) > 0 {
			e["models"] = k.Models
		}
		if len(k.Accounts) > 0 {
			e["accounts"] = k.Accounts
		}
		if k.Limit != nil {
			e["limit"] = k.Limit
		}
		if k.ReadOnly {
			e["readOnly"] = true
		}
		out = append(out, e)
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": out})
}

func (s *Server) revokeIssuedKey(w http.ResponseWriter, r *http.Request) {
	by, status, why := keyIssuer(r)
	if why != "" {
		writeError(w, provider.Chat, status, why)
		return
	}
	if err := access.RevokeIssued(r.PathValue("id"), by); err != nil {
		writeError(w, provider.Chat, http.StatusNotFound, "no key issued here has the id "+strconv.Quote(r.PathValue("id")))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revoked": r.PathValue("id")})
}

// parseTTL reads a ttl given as a duration ("1h") or seconds (3600).
func parseTTL(raw json.RawMessage) (time.Duration, error) {
	bad := errors.New("ttl is how long the key lives, such as \"1h\", \"30m\" or 3600 (seconds), from 1m to 720h")
	if len(raw) == 0 {
		return 0, bad
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		d, err := time.ParseDuration(strings.TrimSpace(s))
		if err != nil {
			return 0, bad
		}
		return d, nil
	}
	var n float64
	if json.Unmarshal(raw, &n) != nil || n <= 0 || n > access.MaxTTL.Seconds() {
		return 0, bad
	}
	return time.Duration(n * float64(time.Second)), nil
}

// keys issued are removed a while after they expire (access.Prune)
// while the gateway serves; issuing one removes them too.
func init() {
	WhileServing = append(WhileServing, func(ctx context.Context) {
		tick := time.NewTicker(10 * time.Minute)
		defer tick.Stop()
		for {
			if n, err := access.Prune(); err != nil {
				log.Printf("issued gateway key cleanup failed: %v", err)
			} else if n > 0 {
				log.Printf("removed %d expired gateway keys", n)
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	})
}
