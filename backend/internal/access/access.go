// Package access keeps the named API keys issued by the gateway.
package access

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/filelock"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

const Prefix = "sk-uni-switch-key-"
const legacyLANPrefix = "sk-uni-switch-"
const revokedLANPrefix = "sk-uni-switch-revoked-"

func Managed(secret string) bool {
	return strings.HasPrefix(secret, legacyLANPrefix)
}

// Named: secret has a named gateway key's form (sk-uni-switch-…), or is the
// key's own value a user gave it (Change.Secret), so a request from this
// computer with one is counted and limited as that key, as a sk-uni-switch-
// key is.
func Named(secret string) bool {
	if Managed(secret) {
		return true
	}
	if secret == "" {
		return false
	}
	own := ownSecrets()
	_, ok := own[secret]
	return ok
}

// own caches the key store's own-value secrets by the file's size and
// time, so a request from this computer doesn't read it each time.
var own struct {
	sync.Mutex
	size    int64
	mod     int64
	secrets map[string]struct{}
}

func ownSecrets() map[string]struct{} {
	fi, err := os.Stat(Path())
	if err != nil {
		return nil
	}
	own.Lock()
	defer own.Unlock()
	if own.secrets != nil && own.size == fi.Size() && own.mod == fi.ModTime().UnixNano() {
		return own.secrets
	}
	mu.Lock()
	keys, err := load()
	mu.Unlock()
	if err != nil {
		return nil // unknown: tried again on the next request
	}
	m := map[string]struct{}{}
	for _, k := range keys {
		if k.Secret != "" && !Managed(k.Secret) {
			m[k.Secret] = struct{}{}
		}
	}
	own.size, own.mod, own.secrets = fi.Size(), fi.ModTime().UnixNano(), m
	return m
}

// ownSecret checks a key value a user brings (one their clients already
// send, from another gateway they move from, such as CLIProxyAPI's
// api-keys, which are any string): what an HTTP header carries, and no
// other key's.
func ownSecret(secret string, keys []Key) (string, error) {
	secret = strings.TrimSpace(secret)
	if n := len(secret); n < 8 || n > 256 {
		return "", errors.New("Use a key between 8 and 256 characters")
	}
	for i := 0; i < len(secret); i++ {
		if c := secret[i]; c <= ' ' || c > '~' {
			return "", errors.New("A key can have only letters, digits and ASCII symbols, no spaces")
		}
	}
	if strings.HasPrefix(secret, legacyLANPrefix) {
		return "", errors.New("A key of your own can't start with sk-uni-switch-: leave the key empty and uni-switch makes one")
	}
	for _, k := range keys {
		if subtle.ConstantTimeCompare([]byte(secret), []byte(k.Secret)) == 1 {
			return "", errors.New("Another gateway key already has this key")
		}
	}
	if upstreamKey(secret) {
		return "", errors.New("This is one of your providers' API keys: give the gateway key a value of its own")
	}
	return secret, nil
}

// upstreamKey: secret is a key uni-switch sends a provider, which a gateway
// key handed to others must never be.
func upstreamKey(secret string) bool {
	for _, p := range provider.All() {
		if p.Key != "" && subtle.ConstantTimeCompare([]byte(secret), []byte(p.Key)) == 1 {
			return true
		}
		for _, k := range p.Keys {
			if k.Key != "" && subtle.ConstantTimeCompare([]byte(secret), []byte(k.Key)) == 1 {
				return true
			}
		}
	}
	return false
}

type Key struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Off    bool   `json:"off,omitempty"`
	LAN    bool   `json:"lan,omitempty"` // default key and durable migration marker
	Secret string `json:"secret,omitempty"`
	Masked string `json:"masked,omitempty"`
	// Limit is the key's own budget (#585); nil for none.
	Limit *Limit `json:"limit,omitempty"`
	// More are its limits after the first (#2227), each a window of its
	// own: a request passes only while every one has something left. A
	// key has them only beside a Limit, which an older uni-switch still reads.
	More []Limit `json:"moreLimits,omitempty"`
	// Models are the models the key may use (#882): "<provider>/<model>",
	// "<provider>/*" or a group's "group/<name>"; none for every model,
	// unless it lists accounts: then its accounts' providers' (HeldModels).
	Models []string `json:"models,omitempty"`
	// Accounts are the accounts and keys the key may use (#905), as
	// "<provider>/<account>": a signed-in account by its stable id, a
	// key by its fingerprint; none for every account, as keys always did.
	Accounts []string `json:"accounts,omitempty"`
	// MCP are the MCP servers and tools the key may use (mcp.go):
	// "<server>", "<server>/<tool>", or NoMCP alone for none; none
	// listed for every server and tool.
	MCP []string `json:"mcp,omitempty"`
	// Quotas lets a key held to a budget, models or accounts be told the
	// quotas of what it may use (#2216): the cards of the providers whose
	// every model it may use, of the accounts and keys it may use, names
	// and balances included. Off, it is told none (#1981).
	Quotas bool `json:"quotas,omitempty"`
	// ReadOnly holds the key to reading this uni-switch's stats (Steven on
	// Discord: a desk display polling quotas and usage, its key kept in
	// a device's flash): the gateway answers it only the routes in its
	// readOnlyRoutes and refuses it every other, every model, MCP and
	// write route among them. Off, the key may do what its other limits let it.
	ReadOnly bool `json:"readOnly,omitempty"`
	// Browser holds the key to connecting the uni-switch Browser extension
	// (GET /v1/uni-switch/browser/extension) and nothing else: the key the
	// GUI writes into the extension's zip (gui's browserKey), which lands
	// in Downloads, where anyone who reads it has it. The gateway refuses
	// it every other route, every model and MCP server among them
	// (readOnlyGuard). Such a key is ReadOnly too, so a uni-switch from before
	// this field, which reads it as any other key, holds it to reading
	// stats rather than handing it every model.
	Browser bool `json:"browser,omitempty"`
	// Expires is when the key stops working (an issued key, IssueKey);
	// zero for one that doesn't. Prune removes it a while after.
	Expires time.Time `json:"expires,omitzero"`
	// Issuer is the id of the key it was issued under, which bounds it
	// (issue.go); "" for one the user made or issued themselves.
	Issuer string `json:"issuer,omitempty"`
	// Issues lets the key issue short-lived keys under itself
	// (POST /v1/uni-switch/keys); an issued key never can.
	Issues bool `json:"issues,omitempty"`
}

// Identity is the gateway key a request came with, its budget, the
// models and the accounts it may use.
type Identity struct {
	KeyID, KeyName string
	Limit          *Limit
	More           []Limit
	Models         []string
	// AccountModels says Models are only HeldModels' "<provider>/*" of
	// a key that names accounts and no model, nor does the key it was
	// issued under: every model of those accounts' providers is the
	// key's, a routing group of them alone too. A key that lists models
	// is given a routing group of the user's only by naming it (kautism
	// on Discord).
	AccountModels bool
	Accounts      []string
	// MCP are the MCP servers and tools the key may use (Key.MCP); an
	// issued key is held to its issuer's too (Issuer.MCP)
	MCP      []string
	Quotas   bool
	ReadOnly bool
	// Browser: the key may connect the uni-switch Browser extension and do
	// nothing else (Key.Browser).
	Browser bool
	// sum is the SHA-256 of the secret the request came with, so a
	// connection kept open can tell the key was rotated since (Current).
	sum [32]byte
	// Expires is when the key stops working, its issuer's expiry if
	// sooner; zero for never.
	Expires time.Time
	// Issuer is the key it was issued under: its limits hold this key's
	// requests too, and count what this key spends.
	Issuer *Identity
	// Issues: the key may issue keys under itself.
	Issues bool
	// Default: the key is the sharing key uni-switch made itself (Default),
	// the one it writes into this computer's own agents that reach the
	// gateway from beyond loopback, never one the user made for someone
	// else.
	Default bool
	// Group is the routing group, by name, whose own accounts hold the
	// request as well (HeldToGroup); "" when none does.
	Group string
	// Spent are the key's limits of some models (Limit.Models) with
	// nothing left, as the gateway found them for this request: their
	// models are left out of its candidates, a routing group's members
	// too, and a request naming one is refused with Why.
	Spent []SpentScope
}

// SpentScope is a limit of some models with nothing left until Reset.
type SpentScope struct {
	// Models are the limit's models, with the other ids each goes by
	Models []string
	Reset  time.Time
	// Why is what a request for one of them is told
	Why string
}

// SpentOn is the spent limit that counts model ("<provider>/<model>"),
// the one that resets last when several do.
func (who Identity) SpentOn(model string) (SpentScope, bool) {
	var out SpentScope
	found := false
	for _, s := range who.Spent {
		if ModelAllowed(s.Models, model) && (!found || s.Reset.After(out.Reset)) {
			out, found = s, true
		}
	}
	return out, found
}

type contextKey struct{}

func WithIdentity(ctx context.Context, who Identity) context.Context {
	return context.WithValue(ctx, contextKey{}, who)
}
func Caller(ctx context.Context) Identity { who, _ := ctx.Value(contextKey{}).(Identity); return who }

// mu is held for every read of caller-keys.json a change is made from,
// and the write, across uni-switchs too (#1674): a key revoked with uni-switch
// keys while the window adds one is never written back.
var mu = filelock.Mutex{Path: func() string { return Path() + ".lock" }}

func Path() string { return filepath.Join(settings.Dir(), "caller-keys.json") }

func load() ([]Key, error) {
	b, err := os.ReadFile(Path())
	if errors.Is(err, os.ErrNotExist) {
		return []Key{}, nil
	}
	if err != nil {
		return nil, err
	}
	var keys []Key
	err = json.Unmarshal(b, &keys)
	return keys, err
}

// List never returns credentials; only the administrator's copy action does.
func List() ([]Key, error) {
	mu.Lock()
	defer mu.Unlock()
	keys, err := load()
	if err != nil {
		return nil, err
	}
	for i := range keys {
		k := &keys[i]
		if len(k.Secret) > 8 {
			prefix := legacyLANPrefix
			if strings.HasPrefix(k.Secret, Prefix) {
				prefix = Prefix
			}
			k.Masked = prefix + "…" + k.Secret[len(k.Secret)-6:]
		}
		if k.Secret != "" && !Managed(k.Secret) { // a key of the user's own: only its end
			k.Masked = "…"
			if len(k.Secret) >= 12 {
				k.Masked += k.Secret[len(k.Secret)-4:]
			}
		}
		k.Secret = ""
	}
	return keys, nil
}

// DefaultSecret reads an existing default named key for config preservation.
// It never creates a key or enables a listener.
func DefaultSecret() string {
	mu.Lock()
	defer mu.Unlock()
	s := settings.Load()
	keys, err := load()
	if err != nil {
		return ""
	}
	for _, k := range keys {
		if k.ID == s.LANKeyID || k.LAN {
			if k.Off {
				return ""
			}
			return k.Secret
		}
	}
	if s.LANKeyID == "" && !strings.HasPrefix(s.LANKey, revokedLANPrefix) {
		return s.LANKey
	}
	return ""
}

// Default recognizes the legacy default key for client-config recovery.
// It neither creates a key nor enables a listener.
func Default(secret string) bool {
	if secret == "" {
		return false
	}
	mu.Lock()
	defer mu.Unlock()
	s := settings.Load()
	keys, err := load()
	if err == nil {
		if i := slices.IndexFunc(keys, func(k Key) bool { return k.LAN || k.ID == s.LANKeyID }); i >= 0 {
			return subtle.ConstantTimeCompare([]byte(secret), []byte(keys[i].Secret)) == 1
		}
	}
	return s.LANKey != "" && !strings.HasPrefix(s.LANKey, revokedLANPrefix) &&
		subtle.ConstantTimeCompare([]byte(secret), []byte(s.LANKey)) == 1
}

// Export returns credentials only for the encrypted backup bundle.
func Export() ([]Key, error) {
	mu.Lock()
	defer mu.Unlock()
	return load()
}

// Restore replaces the gateway credentials when restoring their settings.
func Restore(keys []Key) error {
	mu.Lock()
	defer mu.Unlock()
	return save(slices.Clone(keys))
}

func random(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

type Change struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	// Limit is what "limit-key" sets; nil or Unlimited takes the limit off.
	Limit *Limit `json:"limit,omitempty"`
	// Index is which of the key's limits "limit-key" sets and
	// "reset-limit-key" resets (#2227), counted from 0: one past the last
	// adds a limit, a nil Limit there removes that one. Without it,
	// "limit-key" makes Limit the key's only limit, and "reset-limit-key"
	// resets every one. For "move-key" it is the place in the list the key
	// goes to, counted from 0.
	Index *int `json:"index,omitempty"`
	// Models is what "models-key" sets; none lets the key use every model.
	Models []string `json:"models,omitempty"`
	// Accounts is what "accounts-key" sets (#905); none lets the key use
	// every account and key.
	Accounts []string `json:"accounts,omitempty"`
	// MCP is what "mcp-key" sets: none lets the key use every MCP server
	// and tool, NoMCP alone none of them.
	MCP []string `json:"mcp,omitempty"`
	// Quotas is what "quotas-key" sets (#2216).
	Quotas bool `json:"quotas,omitempty"`
	// ReadOnly is what "access-key" sets, and "add-key" gives the new key:
	// it may read stats and use no model (Key.ReadOnly).
	ReadOnly bool `json:"readOnly,omitempty"`
	// Browser is what "add-key" gives the new key: it may connect the
	// uni-switch Browser extension alone (Key.Browser).
	Browser bool `json:"browser,omitempty"`
	// Issues is what "issuer-key" sets: the key may issue short-lived
	// keys under itself (Key.Issues).
	Issues bool `json:"issues,omitempty"`
	// Secret is the value "add-key" gives the new key, one its clients
	// already send (love1sbug on X: keys handed out from another gateway),
	// or "rotate-key" gives the key; "" for one uni-switch makes.
	Secret string `json:"secret,omitempty"`
}

// Now is when Update sets or resets a limit. A variable for the tests.
var Now = time.Now

// Update writes the named key store atomically.
func Update(action string, in Change) (string, error) {
	mu.Lock()
	defer mu.Unlock()
	keys, err := load()
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(in.Name)
	if action == "add-key" || action == "rename-key" {
		if name == "" || utf8.RuneCountInString(name) > 120 {
			return "", errors.New("Use a name between 1 and 120 characters")
		}
	}
	var secret string
	var mirror *Key
	if action == "add-key" {
		id, err := random(12)
		if err != nil {
			return "", err
		}
		if in.Secret != "" {
			if secret, err = ownSecret(in.Secret, keys); err != nil {
				return "", err
			}
		} else {
			token, err := random(24)
			if err != nil {
				return "", err
			}
			secret = Prefix + token
		}
		keys = append(keys, Key{ID: id, Name: name, Secret: secret, ReadOnly: in.ReadOnly || in.Browser, Browser: in.Browser})
	} else {
		i := slices.IndexFunc(keys, func(k Key) bool { return k.ID == in.Key })
		if i < 0 {
			return "", errors.New("Key not found")
		}
		defaultKey := keys[i].LAN || keys[i].ID == settings.Load().LANKeyID
		if defaultKey {
			keys[i].LAN = true
		}
		switch action {
		case "rotate-key":
			// a value of the user's own (isaac_bai on Discord: one easy to
			// remember and pass on), checked as add-key's is; none, a new
			// one uni-switch makes
			if in.Secret != "" {
				if secret, err = ownSecret(in.Secret, slices.Delete(slices.Clone(keys), i, i+1)); err != nil {
					return "", err
				}
			} else {
				token, err := random(24)
				if err != nil {
					return "", err
				}
				secret = Prefix + token
			}
			keys[i].Secret = secret
		case "rename-key":
			keys[i].Name = name
		case "limit-key":
			lim, err := in.Limit.Valid()
			if err != nil {
				return "", err
			}
			ls := keys[i].limitList()
			now := Now().Round(0)
			if in.Index == nil {
				var first *Limit
				if len(ls) > 0 {
					first = &ls[0]
				}
				ls = nil
				if lim != nil {
					lim.Since = sinceOf(first, lim, now)
					ls = []Limit{*lim}
				}
			} else {
				n := *in.Index
				if n < 0 || n > len(ls) || (n == len(ls) && lim == nil) {
					return "", errors.New("This key has no such limit")
				}
				switch {
				case lim == nil:
					ls = slices.Delete(ls, n, n+1)
				case n == len(ls):
					if len(ls) >= MaxLimits {
						return "", fmt.Errorf("A key can have at most %d limits", MaxLimits)
					}
					lim.Since = sinceOf(nil, lim, now)
					ls = append(ls, *lim)
				default:
					lim.Since = sinceOf(&ls[n], lim, now)
					ls[n] = *lim
				}
			}
			for a := range ls {
				for b := a + 1; b < len(ls); b++ {
					if ls[a].sameLimit(&ls[b]) {
						if ls[a].Scoped() {
							return "", fmt.Errorf("This key already has a limit %s for %s: change that one instead", ls[a].Per(), strings.Join(ls[a].Models, ", "))
						}
						return "", fmt.Errorf("This key already has a limit %s: change that one instead", ls[a].Per())
					}
				}
			}
			keys[i].setLimits(ls)
		case "reset-limit-key":
			// what the key has used is counted again from now (#1509),
			// its tokens and cost together: a "days" or "hours" cycle starts anew,
			// a calendar window keeps its end; one limit of several, or
			// every one (#2227)
			ls := keys[i].limitList()
			if len(ls) == 0 {
				return "", errors.New("This key has no limit to reset")
			}
			now := Now().Round(0)
			if in.Index != nil {
				n := *in.Index
				if n < 0 || n >= len(ls) {
					return "", errors.New("This key has no such limit")
				}
				ls[n].Since = now
			} else {
				for j := range ls {
					ls[j].Since = now
				}
			}
			keys[i].setLimits(ls)
		case "models-key":
			ms, err := CleanModels(in.Models)
			if err != nil {
				return "", err
			}
			keys[i].Models = ms
		case "accounts-key":
			as, err := CleanAccounts(in.Accounts)
			if err != nil {
				return "", err
			}
			keys[i].Accounts = as
		case "mcp-key":
			mcp, err := CleanMCP(in.MCP)
			if err != nil {
				return "", err
			}
			keys[i].MCP = mcp
		case "quotas-key":
			keys[i].Quotas = in.Quotas
		case "issuer-key":
			// may issue short-lived keys under itself (POST /v1/uni-switch/keys)
			if in.Issues && keys[i].Browser {
				return "", errors.New("This key is the uni-switch Browser extension's, which can't issue keys: let another key of yours issue them")
			}
			if in.Issues && (keys[i].Issuer != "" || !keys[i].Expires.IsZero()) {
				return "", errors.New("An issued key can't issue keys of its own: let a key of yours issue them")
			}
			keys[i].Issues = in.Issues
		case "access-key":
			// the extension's key is in a zip in Downloads: it connects the
			// extension and nothing else, whatever access is asked
			if keys[i].Browser {
				return "", errors.New("This key is the uni-switch Browser extension's: it connects the extension and nothing else. Add another key for models or stats")
			}
			keys[i].ReadOnly = in.ReadOnly
		case "on-key", "off-key":
			keys[i].Off = action == "off-key"
		case "remove-key":
			if defaultKey {
				k := keys[i]
				k.Off = true
				mirror = &k
			}
			keys = slices.Delete(keys, i, i+1)
		case "move-key":
			// the keys are listed in the order they are kept in, which the
			// user sets by dragging one (alextjy on Discord); nothing else
			// about the key changes
			if in.Index == nil || *in.Index < 0 || *in.Index >= len(keys) {
				return "", errors.New("There is no such place in the list of keys")
			}
			k := keys[i]
			keys = slices.Insert(slices.Delete(keys, i, i+1), *in.Index, k)
		case "copy-key":
			return keys[i].Secret, nil
		default:
			return "", fmt.Errorf("unknown key action %q", action)
		}
		if defaultKey && action != "remove-key" && action != "rename-key" && action != "limit-key" && action != "reset-limit-key" && action != "models-key" && action != "accounts-key" && action != "mcp-key" && action != "quotas-key" && action != "access-key" && action != "issuer-key" && action != "move-key" {
			mirror = &keys[i]
		}
	}
	if mirror != nil {
		s := settings.Load()
		if err := setLegacyMirror(&s, *mirror); err != nil {
			return "", err
		}
		// Revoke the old-version mirror before changing the active key store.
		if err := settings.Save(s); err != nil {
			return "", fmt.Errorf("Cannot change the default gateway key without updating settings.json: older uni-switch versions may still accept its old credential. Make the settings file writable and retry; the key is unchanged: %w", err)
		}
	}
	if err := save(keys); err != nil {
		return "", err
	}
	return secret, nil
}

func setLegacyMirror(s *settings.Settings, k Key) error {
	s.LANKeyID = k.ID
	if !k.Off {
		s.LANKey = k.Secret
		return nil
	}
	token, err := random(24)
	if err != nil {
		return err
	}
	s.LANKey = revokedLANPrefix + token
	return nil
}

// Authenticate reloads the store so revocation takes effect in running
// gateways. An expired key, or one whose issuer no longer lets it in, is
// refused as one turned off is; Check says why.
func Authenticate(secret string) (Identity, bool) {
	who, err := Check(secret)
	return who, err == nil
}

// ProveSecret is the enabled gateway key whose sha256 is id (hex), for a
// uni-switch's proof that it holds the key (provider.RemoteProvePath): one
// Authenticate takes, found by constant-time compares over every key, so
// which one matched, or whether any did, takes no different time. A key
// turned off, expired or whose issuer no longer lets it in, and a store
// that can't be read, have none.
func ProveSecret(id string) (string, bool) {
	want, err := hex.DecodeString(id)
	if err != nil || len(want) != sha256.Size {
		return "", false
	}
	mu.Lock()
	keys, err := load()
	s := settings.Load()
	mu.Unlock()
	if err != nil {
		return "", false
	}
	secrets := make([]string, 0, len(keys)+1)
	for _, k := range keys {
		secrets = append(secrets, k.Secret)
	}
	// the sharing key an older uni-switch kept in the settings alone, which
	// Check takes while the store has none marked
	if s.LANKey != "" {
		secrets = append(secrets, s.LANKey)
	}
	found := ""
	for _, secret := range secrets {
		sum := sha256.Sum256([]byte(secret))
		if subtle.ConstantTimeCompare(sum[:], want) == 1 && secret != "" && found == "" {
			found = secret
		}
	}
	if found == "" {
		return "", false
	}
	if _, err := Check(found); err != nil {
		return "", false
	}
	return found, true
}

// On: the key with this id is one Authenticate takes, in the store and not
// turned off, or the legacy sharing key not yet written there; false when
// the store can't be read.
func On(id string) bool {
	mu.Lock()
	defer mu.Unlock()
	keys, err := load()
	if err != nil || id == "" {
		return false
	}
	for _, k := range keys {
		if k.ID == id {
			// an issued key is on while it hasn't expired and its issuer
			// lets it in, as Check takes it
			if k.Off || k.Secret == "" || k.expired(Now()) {
				return false
			}
			_, err := identityOf(k, keys, Now())
			return err == nil
		}
	}
	s := settings.Load()
	if s.LANKeyID == "" && s.LANKey != "" && !strings.HasPrefix(s.LANKey, revokedLANPrefix) && !slices.ContainsFunc(keys, func(k Key) bool { return k.LAN }) {
		return legacyLANKey(s.LANKey).ID == id
	}
	return false
}

func legacyLANKey(secret string) Key {
	// The fallback and eventual persisted key must share their usage identity.
	sum := sha256.Sum256([]byte(secret))
	return Key{ID: "lan-" + hex.EncodeToString(sum[:12]), Name: "uni-switch", LAN: true, Secret: secret}
}

// MigrateLegacyLANKey makes the old Settings key a normal, revocable caller
// key. Keep LANKey for older uni-switch versions; LANKeyID marks a completed
// migration so the retained credential cannot resurrect a removed key.
func MigrateLegacyLANKey() error {
	mu.Lock()
	defer mu.Unlock()
	return migrateLegacyLANKey()
}

func MigrateLegacyLANKeyBestEffort() {
	if err := MigrateLegacyLANKey(); err != nil {
		log.Printf("uni-switch: could not migrate LAN key: %v", err)
	}
}

func migrateLegacyLANKey() error {
	s := settings.Load()
	if s.LANKey == "" || s.LANKeyID != "" || strings.HasPrefix(s.LANKey, revokedLANPrefix) {
		return nil
	}
	keys, err := load()
	if err != nil {
		return err
	}
	if slices.ContainsFunc(keys, func(k Key) bool { return k.LAN }) {
		return nil // the key-store write succeeded, even if settings were read-only
	}
	i := slices.IndexFunc(keys, func(k Key) bool {
		return subtle.ConstantTimeCompare([]byte(k.Secret), []byte(s.LANKey)) == 1
	})
	if i < 0 {
		keys = append(keys, legacyLANKey(s.LANKey))
		i = len(keys) - 1
	}
	keys[i].LAN = true
	if err := save(keys); err != nil {
		return err
	}
	s.LANKeyID = keys[i].ID
	return settings.Save(s)
}

func save(keys []Key) error {
	b, err := json.MarshalIndent(keys, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(Path()), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(Path(), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(Path(), 0o600); err != nil {
		return err
	}
	return edit.WriteAtomic(Path(), append(b, '\n'))
}
