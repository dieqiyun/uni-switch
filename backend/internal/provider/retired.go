package provider

// A model its vendor has retired, though the vendor's own list still names
// it (MOMO on Discord: OpenCode Zen's /v1/models still lists exo-free,
// and a request for it gets 410 {"type":"error","error":{"type":
// "ModelDeprecated","message":"Model exo-free has been deprecated."}}).
// The gateway tells Retire when a provider answers so; the model is then
// left out of the catalog — /v1/models, every agent's model list, the
// routing groups uni-switch finds — for that provider only, until it answers
// a request again or retiredFor has passed. A request that names it as
// "provider/model" is still sent, so the vendor's answer is what the
// agent is told, and a model back in service is seen at once.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// retiredFor is how long a model retired so stays out of the lists: the
// vendor's list is what brings it back, and the vendor's list is what
// still has it, so a refresh of the list can't be the end of it. A week
// later it is listed again, and the next request it gets refused so
// takes it out again.
const retiredFor = 7 * 24 * time.Hour

var retired struct {
	sync.Mutex
	at   map[string]time.Time // "provider/model" → when the vendor said it is retired
	from string               // the file read, and its time then
	mod  time.Time
	// checked is when the file was last looked at: within retiredLook it
	// isn't looked at again. Every answered request asks Unretire, and a
	// look at the disk each time was a stat per request; this process's
	// own writes keep retired.at current, and another uni-switch's is seen
	// within retiredLook.
	checked time.Time
	looks   int // looks at the disk, for tests
}

var retiredLook = time.Second

func retiredPath() string { return filepath.Join(filepath.Dir(Path()), "retired.json") }

// readRetired brings retired.at up to the file, when the file changed since
// it was read: after a restart, or written by another uni-switch. Called with
// retired held.
func readRetired() {
	path := retiredPath()
	if path == retired.from && !retired.checked.IsZero() && time.Since(retired.checked) < retiredLook {
		return
	}
	retired.looks++
	retired.checked = time.Now()
	fi, err := os.Stat(path)
	if err != nil {
		if path != retired.from {
			retired.at, retired.from, retired.mod = nil, path, time.Time{}
		}
		return
	}
	if path == retired.from && fi.ModTime().Equal(retired.mod) {
		return
	}
	retired.from, retired.mod = path, fi.ModTime()
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var saved map[string]time.Time
	if json.Unmarshal(b, &saved) != nil {
		return // unreadable: what was read before stands
	}
	retired.at = saved
}

// writeRetired saves retired.at. Called with retired held.
func writeRetired() {
	now := time.Now()
	for k, at := range retired.at {
		if now.Sub(at) >= retiredFor {
			delete(retired.at, k)
		}
	}
	b, err := json.MarshalIndent(retired.at, "", "  ")
	if err != nil {
		return
	}
	path := retiredPath()
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil || writeFileAtomic(path, b) != nil {
		return
	}
	if fi, err := os.Stat(path); err == nil {
		retired.from, retired.mod, retired.checked = path, fi.ModTime(), time.Now()
	}
}

// Retire notes that provider pid's vendor said model is retired: it leaves
// the lists, and the agents' lists are written again.
func Retire(pid, model string) {
	if pid == "" || model == "" {
		return
	}
	k := pid + "/" + model
	retired.Lock()
	readRetired()
	at, was := retired.at[k]
	if was && time.Since(at) < retiredFor {
		retired.Unlock()
		return
	}
	if retired.at == nil {
		retired.at = map[string]time.Time{}
	}
	retired.at[k] = time.Now()
	writeRetired()
	retired.Unlock()
	catalog.Touched()
}

// Unretire takes back a retirement of provider pid's model, when it has
// one: the vendor answered a request for it.
func Unretire(pid, model string) {
	k := pid + "/" + model
	retired.Lock()
	readRetired()
	if _, was := retired.at[k]; !was {
		retired.Unlock()
		return
	}
	delete(retired.at, k)
	writeRetired()
	retired.Unlock()
	catalog.Touched()
}

// Retired says whether provider pid's vendor said model is retired, within
// retiredFor.
func Retired(pid, model string) bool {
	retired.Lock()
	defer retired.Unlock()
	readRetired()
	at, ok := retired.at[pid+"/"+model]
	return ok && time.Since(at) < retiredFor
}

// retiredNow is the models retired now, by "provider/model", read once for
// a catalog built.
func retiredNow() map[string]bool {
	retired.Lock()
	defer retired.Unlock()
	readRetired()
	out := map[string]bool{}
	for k, at := range retired.at {
		if time.Since(at) < retiredFor {
			out[k] = true
		}
	}
	return out
}
