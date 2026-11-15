package usage

// Read-only compatibility for historical usage-others files. The Usage
// page can still display existing records; this package no longer exports,
// imports or deletes shared days for synchronization.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/lastgood"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// ThisComputer is the Filter.Computer of the calls made here.
const ThisComputer = "this"

// SharedDay is the historical on-disk format for one computer's day.
type SharedDay struct {
	Computer string       `json:"computer"` // its id
	Name     string       `json:"name"`     // what it is called, its host name
	Day      string       `json:"day"`      // 2006-01-02, the day where it is
	Calls    []SharedCall `json:"calls"`
}

// SharedCall is a call as the ledger has it: Source "log" for one read from
// an agent's session file.
type SharedCall struct {
	Record
	Source string `json:"source,omitempty"`
}

func dir() string { return filepath.Dir(provider.Path()) }

// OthersDir is where the other computers' days are kept: one folder each.
func OthersDir() string { return filepath.Join(dir(), "usage-others") }

// computerFile is usage-computer.json.
type computerFile struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// Computer is this computer's id and name, the id made the first time and
// kept in usage-computer.json, which no backup or sync carries.
func Computer() (id, name string) {
	p := filepath.Join(dir(), "usage-computer.json")
	var c computerFile
	if b, err := os.ReadFile(p); err == nil {
		json.Unmarshal(b, &c)
	}
	if !validID(c.ID) {
		b := make([]byte, 8)
		rand.Read(b)
		c.ID = hex.EncodeToString(b)
		os.MkdirAll(dir(), 0o755)
		// a file that doesn't parse held this computer's id, the one its
		// synced days are under: it is kept aside before a new id goes
		// over it, and not written over when it can't be
		if j, err := json.Marshal(c); err == nil && lastgood.KeepUnparsed(p, lastgood.Into[computerFile]) == nil {
			edit.WriteAtomic(p, j)
		}
	}
	name = c.Name
	if name == "" {
		name, _ = os.Hostname()
		name = strings.TrimSuffix(name, ".local")
	}
	if name == "" {
		name = c.ID
	}
	return c.ID, name
}

func validID(id string) bool {
	if len(id) < 8 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !('0' <= r && r <= '9' || 'a' <= r && r <= 'f') {
			return false
		}
	}
	return true
}

// sharedFile is where another computer's day is kept here.
func sharedFile(computer, day string) string {
	return filepath.Join(OthersDir(), computer, day+".json")
}

// nameFile keeps what another computer is called, as its latest day says.
func nameFile(computer string) string { return filepath.Join(OthersDir(), computer, "name") }

// sharedNames are the other computers kept here, by id, and what each is called.
func sharedNames() map[string]string {
	out := map[string]string{}
	kept := SharedKept()
	if len(kept) == 0 {
		return out
	}
	self, _ := Computer()
	for _, k := range kept {
		c, _, _ := strings.Cut(k, "/")
		if c == self || out[c] != "" {
			continue
		}
		name, _ := os.ReadFile(nameFile(c))
		out[c] = strings.TrimSpace(string(name))
		if out[c] == "" {
			out[c] = c
		}
	}
	return out
}

// SharedKept are the other computers' days kept here, as "<id>/<day>".
func SharedKept() []string {
	var out []string
	cs, _ := os.ReadDir(OthersDir())
	for _, c := range cs {
		if !c.IsDir() {
			continue
		}
		days, _ := os.ReadDir(filepath.Join(OthersDir(), c.Name()))
		for _, d := range days {
			if day, ok := strings.CutSuffix(d.Name(), ".json"); ok {
				out = append(out, c.Name()+"/"+day)
			}
		}
	}
	slices.Sort(out)
	return out
}

// sharedSource is one kept day, for the request page to read when it changed.
type sharedSource struct {
	path, computer, day string
	size                int64
	mod                 time.Time
}

// sharedSources are the days kept from day since on (all for zero), and
// whether any is kept at all, of any day; this computer's id is never among them.
func sharedSources(since time.Time) (out []sharedSource, some bool) {
	kept := SharedKept()
	if len(kept) == 0 {
		return nil, false
	}
	self, _ := Computer()
	from := ""
	// A day's calls began where it was a day, and since is the reader's
	// day, which can begin up to 26 hours from that computer's (UTC-12 and
	// UTC+14): two days before.
	if !since.IsZero() {
		from = since.AddDate(0, 0, -2).Format(time.DateOnly)
	}
	for _, k := range kept {
		c, day, _ := strings.Cut(k, "/")
		if c == self || !validID(c) {
			continue
		}
		some = true
		if day < from {
			continue
		}
		p := sharedFile(c, day)
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		out = append(out, sharedSource{path: p, computer: c, day: day, size: fi.Size(), mod: fi.ModTime()})
	}
	return out, some
}

func readShared(p string) (SharedDay, bool) {
	var d SharedDay
	b, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(b, &d) != nil {
		return d, false
	}
	return d, true
}

// sharedRecords are the other computers' calls since since, each with its
// computer.
func sharedRecords(since time.Time) (calls []SharedCall) {
	srcs, _ := sharedSources(since)
	for _, s := range srcs {
		d, ok := readShared(s.path)
		if !ok {
			continue
		}
		for _, c := range d.Calls {
			if !since.IsZero() && c.Time.Before(since) {
				continue
			}
			c.Computer = s.computer
			calls = append(calls, c)
		}
	}
	return calls
}
