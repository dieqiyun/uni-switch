package agent

import (
	"slices"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// RenameProvider gives a provider another id (provider.Rename) and moves
// the agents on one of its models to the same model by the new id, each
// spelled the way the agent spells it, and what else in their configs names
// it (RenameRefs). It answers the agents it moved.
func RenameProvider(from, to string) ([]string, error) {
	from = strings.ToLower(strings.TrimSpace(from))
	to = strings.ToLower(strings.TrimSpace(to))
	moved, err := renameRefs(func(ref string) (string, bool) {
		if rest, ok := strings.CutPrefix(ref, from+"/"); ok && from != to {
			return to + "/" + rest, true
		}
		return "", false
	}, func() error { return provider.Rename(from, to) })
	if err != nil {
		return moved, err
	}
	if from != to {
		for _, a := range Detected() {
			if a.RenameRefs == nil || a.handWired() {
				continue
			}
			ok, err := a.RenameRefs(from, to)
			if err != nil {
				return moved, err
			}
			if ok && !slices.Contains(moved, a.Name) {
				moved = append(moved, a.Name)
			}
		}
	}
	SyncCatalog()
	return moved, nil
}

// renameRefs runs rename and then moves every field of a detected agent
// that was on a model whose ref renamed maps, to that model's new ref,
// spelled as the agent spells it. What the agents are on is read before
// rename: the pickers' options name the model by the ref it has then.
func renameRefs(renamed func(ref string) (string, bool), rename func() error) ([]string, error) {
	type move struct {
		a        *Agent
		key, val string
	}
	var moves []move
	for _, a := range Detected() {
		// a provider named uni-switch written by hand is the user's, the
		// models it names too (handWired)
		if a.handWired() {
			continue
		}
		vals := a.Values()
		for _, f := range a.Fields {
			v := vals[f.Key]
			if v == "" {
				continue
			}
			// the agent's suffix after the model (SplitSuffix) stays; a
			// list of models is the user's own, its names moved by
			// RenameRefs where the agent has one
			model, suffix, one := a.split(v)
			if !one {
				continue
			}
			nv := ""
			if f.Options != nil {
				for _, o := range f.Options(vals) {
					if o.Value != model {
						continue
					}
					if now, ok := renamed(o.Ref); ok {
						nv = strings.Replace(model, o.Ref, now, 1) + suffix
						break
					}
				}
			}
			if ref, ok := strings.CutPrefix(v, uniSwitchID+"/"); nv == "" && ok {
				// the agent's value names uni-switch's model outright
				// (uni-switch/<ref>), with its suffix after it
				for _, r := range []string{ref, strings.TrimPrefix(model, uniSwitchID+"/")} {
					if !strings.HasPrefix(ref, r) {
						continue
					}
					if now, ok := renamed(r); ok {
						nv = uniSwitchID + "/" + now + strings.TrimPrefix(ref, r)
						break
					}
				}
			}
			if nv != "" && nv != v {
				moves = append(moves, move{a, f.Key, nv})
			}
		}
	}
	if err := rename(); err != nil {
		return nil, err
	}
	var moved []string
	for _, m := range moves {
		if err := m.a.Apply(m.key, m.val); err != nil {
			return moved, err
		}
		if len(moved) == 0 || moved[len(moved)-1] != m.a.Name {
			moved = append(moved, m.a.Name)
		}
	}
	return moved, nil
}

// MoveOffAccountIDs moves each of the user's providers saved on a
// subscription's id (provider.OnAccountIDs) to a free one, the agents on it
// with it, so the subscription is listed once signed in: a "WorkBuddy" key
// saved as workbuddy before WorkBuddy's plan was a subscription hid it, and
// signing in to it said "signed in, but uni-switch can't list it". Run at
// start-up; errors are swallowed as RenameLegacy's are.
func MoveOffAccountIDs() {
	for _, id := range provider.OnAccountIDs() {
		_, _ = RenameProvider(id, provider.FreeID(id))
	}
}

// renameRef is one entry of an agent's enabledModels (StepCode's, Pi's
// and its forks') with uni-switch's provider from renamed to to. A glob or a thinking level after the model stays. An entry that
// does not name that provider is the user's and is left as it is.
func renameRef(s, from, to string) (string, bool) {
	rest, ok := strings.CutPrefix(s, uniSwitchID+"/"+from+"/")
	if !ok {
		return s, false
	}
	return uniSwitchID + "/" + to + "/" + rest, true
}

// renameRefList renames uni-switch/<from>/ entries. The model just set is
// appended before the old entry is renamed onto the same text, so that
// second copy is dropped. A list that names nothing of that provider is
// returned unchanged, a repeated entry of the user's included.
func renameRefList(pats []string, from, to string) ([]string, bool) {
	if len(pats) == 0 {
		return pats, false
	}
	out := make([]string, 0, len(pats))
	seen := map[string]bool{}
	fromRename := map[string]bool{}
	renamed := false
	for _, p := range pats {
		n, hit := renameRef(p, from, to)
		renamed = renamed || hit
		if seen[n] && (hit || fromRename[n]) {
			continue
		}
		if !seen[n] {
			seen[n] = true
			fromRename[n] = hit
		}
		out = append(out, n)
	}
	if !renamed {
		return pats, false
	}
	return out, true
}
