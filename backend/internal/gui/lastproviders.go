package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
)

// The last-known providers list (#1519, fatkun): the Providers page asked
// /api/providers each time it opened, and after a start that read took
// seconds (every account's sign-in, every agent's model), so the page
// showed skeletons for that long. As the Agents page draws the last state
// (laststate.go), it now draws the list built last, kept here and in the
// cache folder for the next start, and the fresh one replaces it the
// moment it is in.
//
// What is kept leaves out what the page never needs before the fresh list
// is in and what can hold a secret: the gateway's recent calls and lanes,
// each provider's headers, proxies and extra body. The page opens no
// provider's editor from the kept list; it waits for the fresh one.

var lastProviders struct {
	sync.Mutex
	kept []byte
	read bool
	sum  [32]byte // kept's keptSum
}

type providersFile struct {
	Version   string        `json:"version"`
	Providers providersJSON `json:"providers"`
}

func lastProvidersPath() string { return filepath.Join(appdir.Cache(), "providers-state.json") }

// keepProviders remembers s as the last-known list, written only when what
// is kept changed.
func keepProviders(s providersJSON) {
	sum, err := encodedSum(keptProviders(s)) // as keepState's: no copy to compare
	if err != nil {
		return
	}
	keepProvidersSummed(s, sum)
}

// keptProviders is what keepProviders keeps of s.
func keptProviders(s providersJSON) providersFile {
	s.Gateway.Calls, s.Gateway.Lanes = nil, nil
	s.Moved, s.Added, s.Had, s.Removed, s.Fetching = nil, 0, 0, 0, false
	ps := make([]providerJSON, len(s.Providers))
	for i, p := range s.Providers {
		ps[i] = keptProvider(p)
	}
	s.Providers = ps
	return providersFile{Version: Version, Providers: s}
}

// keptProvider is what is kept of a provider's row.
func keptProvider(p providerJSON) providerJSON {
	p.Headers, p.ExtraBody, p.Proxy, p.AccountProxies = nil, nil, "", nil
	return p
}

// keepsAllOf reports whether p is kept as it is: keptProvider leaves it
// the same.
func keepsAllOf(p *providerJSON) bool {
	return p.Headers == nil && p.ExtraBody == nil && p.Proxy == "" && p.AccountProxies == nil
}

// keepProvidersSummed is keepProviders with the encodedSum of what is kept
// of s already taken (writeProvidersJSON's).
func keepProvidersSummed(s providersJSON, sum [32]byte) {
	lastProviders.Lock()
	defer lastProviders.Unlock()
	if !lastProviders.read {
		lastProviders.kept, _ = os.ReadFile(lastProvidersPath())
		lastProviders.read, lastProviders.sum = true, keptSum(lastProviders.kept)
	}
	if sum == lastProviders.sum {
		return
	}
	keptMarshaled.Add(1)
	b, err := json.Marshal(keptProviders(s))
	if err != nil {
		return
	}
	lastProviders.kept, lastProviders.sum = b, sum
	// only this user's to read: it names their accounts
	if _, err := os.Stat(lastProvidersPath()); os.IsNotExist(err) {
		os.MkdirAll(filepath.Dir(lastProvidersPath()), 0o700)
		os.WriteFile(lastProvidersPath(), nil, 0o600)
	}
	_ = edit.WriteAtomic(lastProvidersPath(), b)
}

// lastKnownProviders is the last list built, by this run or the one
// before it, as kept; false when none is kept, or another version kept it.
func lastKnownProviders() (json.RawMessage, bool) {
	lastProviders.Lock()
	if !lastProviders.read {
		lastProviders.kept, _ = os.ReadFile(lastProvidersPath())
		lastProviders.read, lastProviders.sum = true, keptSum(lastProviders.kept)
	}
	b := lastProviders.kept
	lastProviders.Unlock()
	var f struct {
		Version   string          `json:"version"`
		Providers json.RawMessage `json:"providers"`
	}
	if len(b) == 0 || json.Unmarshal(b, &f) != nil || f.Version != Version || len(f.Providers) == 0 || f.Providers[0] != '{' {
		return nil, false
	}
	// marked, for the page to tell it from a fresh one
	return append(json.RawMessage(`{"last":true,`), f.Providers[1:]...), true
}
