package library

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Setup is which agents get what from the library, and the instructions,
// as a profile keeps them to switch back to. The servers and the skills
// themselves stay in the library: a setup only names them.
type Setup struct {
	Instructions SetupInstructions   `json:"instructions"`
	MCP          map[string][]string `json:"mcp"`    // server → the agents that get it
	Skills       map[string][]string `json:"skills"` // skill → the agents that get it
}

// SetupInstructions is the shared text, the agents it is written to, and
// what each agent gets besides.
type SetupInstructions struct {
	Set    string            `json:"set,omitempty"` // the set it is the text of
	Shared string            `json:"shared,omitempty"`
	Agents []string          `json:"agents"`
	Extra  map[string]string `json:"extra,omitempty"`
}

// Empty reports whether the library had nothing in it when s was taken.
func (s *Setup) Empty() bool {
	i := s.Instructions
	return len(s.MCP) == 0 && len(s.Skills) == 0 && i.Shared == "" && len(i.Agents) == 0 && len(i.Extra) == 0
}

// Summary is what a setup gives out, in a few words: the servers and
// skills given to at least one agent, and whether any agent gets the
// instructions.
func (s *Setup) Summary() string {
	count := func(m map[string][]string, one, many string) string {
		n := 0
		for _, agents := range m {
			if len(agents) > 0 {
				n++
			}
		}
		if n == 0 {
			return ""
		}
		if n == 1 {
			return "1 " + one
		}
		return fmt.Sprintf("%d %s", n, many)
	}
	var parts []string
	for _, p := range []string{count(s.MCP, "server", "servers"), count(s.Skills, "skill", "skills")} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if s.GivesInstructions() {
		parts = append(parts, "instructions")
	}
	if len(parts) == 0 {
		return "library: nothing on"
	}
	return "library: " + strings.Join(parts, ", ")
}

// GivesInstructions reports whether any agent gets instructions from it.
func (s *Setup) GivesInstructions() bool {
	i := s.Instructions
	if i.Shared != "" && len(i.Agents) > 0 {
		return true
	}
	for _, id := range i.Agents {
		if i.Extra[id] != "" {
			return true
		}
	}
	return false
}

// On counts the servers and the skills a setup gives to at least one agent.
func (s *Setup) On() (servers, skills int) {
	for _, a := range s.MCP {
		if len(a) > 0 {
			servers++
		}
	}
	for _, a := range s.Skills {
		if len(a) > 0 {
			skills++
		}
	}
	return
}

func extras() map[string]string {
	out := map[string]string{}
	es, _ := os.ReadDir(filepath.Dir(extraPath("x")))
	for _, e := range es {
		id, ok := strings.CutSuffix(e.Name(), ".md")
		id = agentOfFile(id)
		if !ok || e.IsDir() || checkAgent(id) != nil {
			continue
		}
		if text := readText(extraPath(id)); text != "" {
			out[id] = text
		}
	}
	return out
}

// Snapshot is the library's setup as it is now.
func Snapshot() (*Setup, error) {
	mu.Lock()
	defer mu.Unlock()
	l, err := load()
	if err != nil {
		return nil, err
	}
	s := &Setup{
		Instructions: SetupInstructions{Set: l.active(), Shared: setText(l.sharedPath()), Agents: slices.Clone(l.Instructions.Agents), Extra: extras()},
		MCP:          map[string][]string{},
		Skills:       map[string][]string{},
	}
	if s.Instructions.Agents == nil {
		s.Instructions.Agents = []string{}
	}
	if len(s.Instructions.Extra) == 0 {
		s.Instructions.Extra = nil
	}
	for _, x := range l.MCP {
		s.MCP[x.Name] = append([]string{}, x.Agents...)
	}
	for _, x := range l.Skills {
		s.Skills[x.Name] = append([]string{}, x.Agents...)
	}
	return s, nil
}

// Holds reports whether the library is as s has it: Restore would put back
// nothing that differs. A server or skill s names that the library no
// longer has isn't compared, as Restore can't give it either, nor one added
// since s was taken, which Restore leaves as it is.
func Holds(s *Setup) bool {
	mu.Lock()
	defer mu.Unlock()
	l, err := load()
	if err != nil {
		return false
	}
	if l.hasSet(s.Instructions.Set) && l.active() != s.Instructions.Set {
		return false
	}
	if setText(l.sharedPath()) != strings.TrimSpace(s.Instructions.Shared) {
		return false
	}
	for id, text := range extras() {
		if strings.TrimSpace(text) != strings.TrimSpace(s.Instructions.Extra[id]) {
			return false
		}
	}
	for id, text := range s.Instructions.Extra {
		if checkAgent(id) == nil && readText(extraPath(id)) != strings.TrimSpace(text) {
			return false
		}
	}
	same := func(a, b []string) bool {
		return slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
	}
	if !same(l.Instructions.Agents, s.Instructions.Agents) {
		return false
	}
	for name, agents := range s.MCP {
		if x := l.server(name); x != nil && !same(x.Agents, agents) {
			return false
		}
	}
	for name, agents := range s.Skills {
		if x := l.skill(name); x != nil && !same(x.Agents, agents) {
			return false
		}
	}
	return true
}

// Restore gives the servers and skills to the agents s names and puts the
// instructions back as they were, then writes the library into the agents.
// A server or skill s names that the library no longer has is left out and
// listed in Result.Missing; one added to the library since s was taken is
// left with the agents it has, as s knows nothing of it. The library's own
// instructions files that change are kept aside with the agents' files,
// in the same backup.
func Restore(s *Setup) (*Result, error) {
	var missing []string
	res, err := change(func(l *Library) error {
		missing = nil
		b := newBackups()
		l.kept = b
		// the text as it is, then as s has it: a file that changes is kept first
		// the set s was taken on, if it is still kept, gets its text back
		if l.hasSet(s.Instructions.Set) {
			l.Instructions.Active = s.Instructions.Set
		}
		texts := map[string]string{l.sharedPath(): s.Instructions.Shared}
		for id := range extras() {
			texts[extraPath(id)] = ""
		}
		for id, text := range s.Instructions.Extra {
			if err := checkAgent(id); err != nil {
				return err
			}
			texts[extraPath(id)] = text
		}
		for p, text := range texts {
			if readText(p) == strings.TrimSpace(text) {
				continue
			}
			if err := b.keep("library", p); err != nil {
				return err
			}
			if err := l.putText(p, text); err != nil {
				return err
			}
		}
		l.Instructions.Agents = slices.Sorted(slices.Values(s.Instructions.Agents))
		for name, agents := range s.MCP {
			if x := l.server(name); x != nil {
				x.Agents = slices.Sorted(slices.Values(agents))
			} else {
				missing = append(missing, "mcp:"+name)
			}
		}
		for name, agents := range s.Skills {
			if x := l.skill(name); x != nil {
				x.Agents = slices.Sorted(slices.Values(agents))
			} else {
				missing = append(missing, "skill:"+name)
			}
		}
		sort.Strings(missing)
		return nil
	})
	if res != nil {
		res.Missing = missing
	}
	return res, err
}

// InUse counts what the library gives agents: each server, skill, hook
// and project folder it keeps, and the shared instructions while an agent
// gets them. The library built-in plugin stays on while it is
// more than 0. A library.json that can't be read is an error, never 0.
//
// It reads without mu: a sync holds mu for as long as it takes (13s and
// more with many agents and skills), and boot.js, which the window waits
// on before it paints, asks this through the built-in plugins (#2774: a
// white window at start on macOS). library.json is only ever replaced
// whole (save's edit.WriteAtomic), so a read beside a sync sees the file
// before or after one of its writes, never half of it.
func InUse() (int, error) {
	l, err := load()
	if err != nil {
		return 0, err
	}
	n := len(l.MCP) + len(l.Skills) + len(l.Hooks) + len(l.Projects)
	if len(l.Instructions.Agents) > 0 {
		n++
	}
	return n, nil
}
