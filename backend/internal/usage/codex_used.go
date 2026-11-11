package usage

import (
	"slices"
	"sync"
	"time"
)

// codexUsedDays is how far back CodexUsed looks.
const codexUsedDays = 30

var codexUsedMemo struct {
	sync.Mutex
	path  string
	at    time.Time
	gen   int // counted up by codexUsedNew: a read begun before it is not kept
	all   []string
	byKey map[string][]string
}

// CodexUsed is the models Codex asked for in the last 30 days, as its
// records name them (a ChatGPT account's own as codex/<slug>), most
// recently used first, at most 100. Codex's model list puts these where the
// Codex app reads it (codexcat.Fit). It is read again at most once a
// minute, the list's ETag being on every reply Codex gets, or at once when
// Codex uses a model it hadn't (codexUsedNew). A call that failed counts:
// the user chose that model all the same.
func CodexUsed() []string {
	all, _ := codexUsed()
	return all
}

// CodexUsedBy is CodexUsed for the Codex calls made with one gateway key
// alone: a key's holder is told nothing of what others used.
func CodexUsedBy(keyID string) []string {
	_, byKey := codexUsed()
	return byKey[keyID]
}

func codexUsed() ([]string, map[string][]string) {
	path, now := Path(), time.Now()
	codexUsedMemo.Lock()
	if codexUsedMemo.path == path && now.Sub(codexUsedMemo.at) < time.Minute && !codexUsedMemo.at.After(now) {
		all, byKey := codexUsedMemo.all, codexUsedMemo.byKey
		codexUsedMemo.Unlock()
		return all, byKey
	}
	gen := codexUsedMemo.gen
	codexUsedMemo.Unlock() // the read is done outside: a reply's ETag waits on the lock
	last := map[string]time.Time{}
	keyLast := map[string]map[string]time.Time{}
	readLogSnapshot().visit(now.Add(-codexUsedDays*24*time.Hour), func(r Record) {
		if !codexChoice(r) {
			return
		}
		if r.Time.After(last[r.Requested]) {
			last[r.Requested] = r.Time
		}
		k := keyLast[r.CallerKeyID]
		if k == nil {
			k = map[string]time.Time{}
			keyLast[r.CallerKeyID] = k
		}
		if r.Time.After(k[r.Requested]) {
			k[r.Requested] = r.Time
		}
	})
	all := mostRecent(last)
	byKey := make(map[string][]string, len(keyLast))
	for id, k := range keyLast {
		byKey[id] = mostRecent(k)
	}
	codexUsedMemo.Lock()
	if codexUsedMemo.gen == gen {
		codexUsedMemo.path, codexUsedMemo.at, codexUsedMemo.all, codexUsedMemo.byKey = path, now, all, byKey
	}
	codexUsedMemo.Unlock()
	return all, byKey
}

// codexChoice reports whether r is a call to a model the user chose in
// Codex.
func codexChoice(r Record) bool {
	return r.Agent == "codex" && r.Kind == "" && !r.IsReported() && r.Requested != ""
}

// codexUsedNew has CodexUsed read again on its next call when Append writes
// a Codex call to a model it doesn't hold, for the call's key or at all.
func codexUsedNew(r Record) {
	if !codexChoice(r) {
		return
	}
	codexUsedMemo.Lock()
	defer codexUsedMemo.Unlock()
	// a list of 100 is full: a model past it waits for the minute
	isNew := func(l []string) bool { return len(l) < 100 && !slices.Contains(l, r.Requested) }
	if isNew(codexUsedMemo.all) || isNew(codexUsedMemo.byKey[r.CallerKeyID]) {
		codexUsedMemo.at = time.Time{}
		codexUsedMemo.gen++
	}
}

// mostRecent is the models in last, the latest first, at most 100.
func mostRecent(last map[string]time.Time) []string {
	used := make([]string, 0, len(last))
	for m := range last {
		used = append(used, m)
	}
	slices.SortFunc(used, func(a, b string) int {
		if c := last[b].Compare(last[a]); c != 0 {
			return c
		}
		if a < b {
			return -1
		}
		if a > b {
			return 1
		}
		return 0
	})
	if len(used) > 100 {
		used = used[:100]
	}
	return used
}
