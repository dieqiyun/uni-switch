package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/steady"
)

// A key out of credit climbs creditLadder (30 minutes up to a week) in
// restingUntil, which is memory: a restart of uni-switch started it over at
// 30 minutes, its rest gone too (PaulT on Discord). Each such rest — its
// rung (Failures) and when it ends — is kept in rests.json beside
// providers.json, and read back once at start (loadRests), so the key
// rests out what is left and climbs on from its rung. Only the ladder's
// rests are kept: the others are a minute to a few hours, or end when the
// vendor said, and a reading finds them again.

// restsPath is the file the ladder's rests are kept in.
func restsPath() string { return filepath.Join(filepath.Dir(provider.Path()), "rests.json") }

// restsKeep: a rest that ended this long ago is let go from the file. In
// memory an ended rest keeps its rung until an answer or "Try now"; a key
// left untried for over a month starts over.
const restsKeep = longestMonthly

// ladderRest says r is a key's rest on creditLadder.
func ladderRest(r Rest) bool { return r.Why == failCredit && r.By == "credit" && r.Failures > 0 }

// savedRest is a ladder rest as rests.json has it.
type savedRest struct {
	Rest
	First bool      `json:"first,omitempty"`
	AskAt time.Time `json:"askAt,omitzero"`
	Agent string    `json:"agent,omitempty"`
	User  string    `json:"user,omitempty"`
	Said  string    `json:"said,omitempty"`
}

var restsSaving struct {
	sync.Mutex
	// last is what was written to path, so that an unchanged set isn't
	// written again
	path string
	last string
}

// restsLoaded: the paths whose file this process read already, read once
// each: a test's HOME is its own.
var restsLoaded = struct {
	sync.Mutex
	from map[string]bool
}{from: map[string]bool{}}

// saveRests writes the ladder's rests to rests.json, through a file
// renamed over it, when they changed. Called after any change that set or
// dropped one, outside restingUntil.
func saveRests() {
	path := restsPath()
	restsSaving.Lock()
	defer restsSaving.Unlock()
	out := map[string]savedRest{}
	now := time.Now()
	restingUntil.Lock()
	for k, until := range restingUntil.m {
		r, ok := restingUntil.note[k]
		if !ok || !ladderRest(r) || now.Sub(until) > restsKeep {
			continue
		}
		r.Until = until
		out[k] = savedRest{Rest: r, First: r.first, AskAt: r.askAt, Agent: r.agent, User: r.user, Said: r.said}
	}
	restingUntil.Unlock()
	b, err := json.Marshal(out)
	if err != nil {
		return
	}
	if restsSaving.path == path && restsSaving.last == string(b) {
		return
	}
	if len(out) == 0 {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			restsSaving.path, restsSaving.last = path, string(b)
			return // none kept, none to write
		}
	}
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	target, err := edit.Target(path) // a symlink stays, its target written
	if err != nil {
		return
	}
	tmp := target + ".uni-switch-tmp"
	if os.WriteFile(tmp, b, 0o600) != nil {
		return
	}
	if steady.Rename(tmp, target) != nil {
		os.Remove(tmp)
		return
	}
	restsSaving.path, restsSaving.last = path, string(b)
}

// loadRests reads the ladder's rests a uni-switch before this one kept, once
// for each file. A rest ended since keeps its rung, as it would have here,
// unless it ended over restsKeep ago; a key no longer in providers.json
// is let go. A file that can't be read or parsed adds nothing: no rest is
// guessed from it, and the next change writes it anew. A rest this
// process has already is kept over the file's.
func loadRests() {
	path := restsPath()
	restsLoaded.Lock()
	defer restsLoaded.Unlock()
	if restsLoaded.from[path] {
		return
	}
	restsLoaded.from[path] = true
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var saved map[string]savedRest
	if json.Unmarshal(b, &saved) != nil {
		return
	}
	known, err := restKeysStored()
	if err != nil {
		return // which keys are there is unknown: none are rested by guess
	}
	now := time.Now()
	restingUntil.Lock()
	for k, v := range saved {
		r := v.Rest
		if !ladderRest(r) || r.Until.IsZero() || now.Sub(r.Until) > restsKeep || !known(k) {
			continue
		}
		if _, ok := restingUntil.m[k]; ok {
			continue
		}
		r.Failures = min(r.Failures, len(creditLadder))
		if longest := now.Add(creditLadder[len(creditLadder)-1]); r.Until.After(longest) {
			r.Until = longest // a clock set back since is no longer rest
		}
		r.Key = k
		r.first, r.askAt, r.agent, r.user, r.said = v.First, v.AskAt, v.Agent, v.User, v.Said
		restingUntil.m[k] = r.Until
		restingUntil.note[k] = r
	}
	restingUntil.Unlock()
}

// restKeysStored says whether a rest key — a provider's id, or id#key,
// either followed by /model — names a key providers.json still has.
func restKeysStored() (func(string) bool, error) {
	ps, err := provider.Stored()
	if err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	for _, p := range ps {
		keys[p.ID] = len(p.KeyList()) > 0
		for _, k := range p.KeyList() {
			keys[p.ID+"#"+k.ID] = true
		}
	}
	return func(k string) bool {
		k, _, _ = strings.Cut(k, "/")
		return keys[k]
	}, nil
}
