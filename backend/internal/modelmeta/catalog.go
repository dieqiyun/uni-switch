package modelmeta

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type catalogProvider struct {
	ID     string                `json:"id"`
	API    string                `json:"api"`
	Models map[string]catalogRow `json:"models"`
}
type catalogRow struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Family      string `json:"family"`
	Released    string `json:"release_date"`
	Canonical   string `json:"canonical_model_id"`
	Reasoning   bool   `json:"reasoning"`
	Temperature *bool  `json:"temperature"`
	Options     []struct {
		Type   string   `json:"type"`
		Values []string `json:"values"`
	} `json:"reasoning_options"`
	Modalities struct {
		Input  []string `json:"input"`
		Output []string `json:"output"`
	} `json:"modalities"`
	Limit struct {
		Context int64 `json:"context"`
		Input   int64 `json:"input"`
		Output  int64 `json:"output"`
	} `json:"limit"`
}
type Catalog struct {
	providers  map[string]catalogProvider
	aggregates map[string]Facts
	listed     map[string]bool
	canonical  map[string]bool
	spellings  map[string]string
	counts     map[string]int
}

func EmptyCatalog() *Catalog { c, _ := ReadCatalog([]byte(`{}`), nil); return c }

func (r catalogRow) efforts(provider string) []string {
	var levels []string
	toggle := false
	for _, option := range r.Options {
		if option.Type == "effort" {
			levels = clone(option.Values)
		}
		toggle = toggle || option.Type == "toggle"
	}
	if toggle && len(levels) > 0 && !includes(levels, "none") && (provider == "deepseek" || provider == "kimi-code-plan-cn" || provider == "kimi-code-plan-global") {
		levels = append([]string{"none"}, levels...)
	}
	return levels
}
func (r catalogRow) budgeted() bool {
	for _, o := range r.Options {
		if o.Type == "budget_tokens" {
			return true
		}
	}
	return false
}
func (r catalogRow) window() int64 {
	in, ctx := Window(r.Limit.Input), Window(r.Limit.Context)
	if in > 0 {
		if ctx > 0 && ctx < in {
			return ctx
		}
		return in
	}
	return ctx
}
func (p catalogProvider) levels(r catalogRow, provider string) []string {
	if levels := r.efforts(provider); len(levels) > 0 || !r.Reasoning || r.Family == "" || r.budgeted() {
		return levels
	}
	var best catalogRow
	var levels []string
	for _, key := range sortedKeys(p.Models) {
		x := p.Models[key]
		if x.Family != r.Family || !x.Reasoning || x.ID == r.ID || r.Released != "" && x.Released > r.Released {
			continue
		}
		if levels != nil && (x.Released < best.Released || x.Released == best.Released && x.ID <= best.ID) {
			continue
		}
		if e := x.efforts(provider); len(e) > 0 {
			best, levels = x, e
		}
	}
	return levels
}
func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ReadCatalog implements catalog.loadGen's cross-provider voting and canonical
// aliases. A provider gets one reasoning-level vote per model/list, even when it
// repeats the model under several upstream paths. Lab-only rows never overrule
// a model already named in a provider's catalog.
func ReadCatalog(data, labs []byte) (*Catalog, error) {
	var ps map[string]catalogProvider
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&ps); err != nil || ps == nil {
		return nil, fmt.Errorf("invalid models.dev catalog")
	}
	c := &Catalog{providers: ps, aggregates: map[string]Facts{}, listed: map[string]bool{}, canonical: map[string]bool{}, spellings: map[string]string{}, counts: map[string]int{}}
	type votes struct {
		vision, reason   int
		windows, outputs map[int64]int
		levels, names    map[string]int
	}
	byID := map[string]*votes{}
	voted := map[string]bool{}
	vote := func(id, own, provider string, r catalogRow) {
		v := byID[id]
		if v == nil {
			v = &votes{windows: map[int64]int{}, outputs: map[int64]int{}, levels: map[string]int{}, names: map[string]int{}}
			byID[id] = v
		}
		c.counts[id]++
		if r.Name != "" && r.Name != own && r.Name != id {
			v.names[r.Name]++
		}
		levels := r.efforts("")
		if len(levels) > 0 {
			list := strings.Join(levels, ",")
			key := provider + "\x00" + id + "\x00" + list
			if !voted[key] {
				voted[key] = true
				v.levels[list]++
			}
		}
		if r.Reasoning || len(levels) > 0 {
			v.reason++
		} else {
			v.reason--
		}
		if includes(r.Modalities.Input, "image") {
			v.vision++
		} else {
			v.vision--
		}
		if n := r.window(); n > 0 {
			v.windows[n]++
		}
		if r.Limit.Output > 0 {
			v.outputs[r.Limit.Output]++
		}
	}
	for _, pid := range sortedKeys(ps) {
		p := ps[pid]
		if p.Models == nil {
			return nil, fmt.Errorf("invalid models.dev provider catalog")
		}
		for _, key := range sortedKeys(p.Models) {
			r := p.Models[key]
			if r.ID == "" {
				r.ID = key
				p.Models[key] = r
			}
			id := bare(key)
			c.listed[id] = true
			if strings.Contains(r.Canonical, "/") {
				c.canonical[bare(r.Canonical)] = true
			}
			vote(id, key, pid, r)
		}
	}
	for _, pid := range sortedKeys(ps) {
		for _, key := range sortedKeys(ps[pid].Models) {
			r := ps[pid].Models[key]
			id := bare(r.Canonical)
			if strings.Contains(r.Canonical, "/") && !c.listed[id] {
				vote(id, key, pid, r)
			}
		}
	}
	var labRows map[string]catalogRow
	if len(labs) > 0 {
		_ = json.Unmarshal(labs, &labRows)
	}
	labOnly := map[string]catalogRow{}
	for _, key := range sortedKeys(labRows) {
		r := labRows[key]
		id := bare(key)
		if c.listed[id] || c.canonical[id] {
			continue
		}
		vote(id, id, "lab:"+key, r)
		if _, exists := labOnly[id]; !exists {
			labOnly[id] = r
		}
	}
	for id := range labOnly {
		c.listed[id] = true
	}
	for _, id := range sortedKeys(byID) {
		v := byID[id]
		f := Facts{ID: id, Name: winningName(v.names), Context: winningNumber(v.windows), Output: winningNumber(v.outputs), Reasoning: v.reason > 0, InferredImages: v.vision > 0}
		if list := winningLevels(v.levels); list != "" {
			f.Efforts = strings.Split(list, ",")
		}
		c.aggregates[id] = f
		spelling := SameModel(id)
		prev, ok := c.spellings[spelling]
		if !ok || c.counts[id] > c.counts[prev] || c.counts[id] == c.counts[prev] && id < prev {
			c.spellings[spelling] = id
		}
	}
	// Lab-only models borrow levels from the newest already-released thinking
	// member of their explicit family (catalog.labKinEfforts).
	for id, r := range labOnly {
		f, ok := c.aggregates[id]
		if !ok || len(f.Efforts) > 0 || !f.Reasoning || r.Family == "" {
			continue
		}
		var best catalogRow
		for _, p := range ps {
			for _, x := range p.Models {
				candidate := c.aggregates[bare(x.ID)]
				if x.Family != r.Family || !candidate.Reasoning || len(candidate.Efforts) == 0 || r.Released != "" && x.Released > r.Released {
					continue
				}
				if best.ID == "" || x.Released > best.Released || x.Released == best.Released && bare(x.ID) > bare(best.ID) {
					best = x
				}
			}
		}
		if best.ID != "" {
			f.Efforts = clone(c.aggregates[bare(best.ID)].Efforts)
			c.aggregates[id] = f
		}
	}
	return c, nil
}
func winningNumber(v map[int64]int) int64 {
	var best int64
	most := 0
	for n, count := range v {
		if count > most || count == most && n < best {
			best, most = n, count
		}
	}
	return best
}
func winningName(v map[string]int) string {
	best := ""
	most := 0
	for s, count := range v {
		if count > most || count == most && (len(s) < len(best) || len(s) == len(best) && s < best) {
			best, most = s, count
		}
	}
	return best
}
func winningLevels(v map[string]int) string {
	best := ""
	most := 0
	for s, count := range v {
		a, b := strings.Count(s, ","), strings.Count(best, ",")
		if count > most || count == most && (a < b || a == b && s < best) {
			best, most = s, count
		}
	}
	return best
}

func (c *Catalog) row(provider, id string) (catalogRow, bool) {
	p, ok := c.providers[provider]
	if !ok {
		return catalogRow{}, false
	}
	want := bare(id)
	for _, variant := range []string{want, strings.Split(want, "(")[0], strings.Split(want, ":")[0]} {
		for _, key := range sortedKeys(p.Models) {
			r := p.Models[key]
			if bare(key) == variant || bare(r.Canonical) == variant && r.Canonical != "" {
				return r, true
			}
		}
	}
	for _, key := range sortedKeys(p.Models) {
		if SameModel(key) == SameModel(want) {
			return p.Models[key], true
		}
	}
	return catalogRow{}, false
}
func rowFacts(id, pid string, r catalogRow, p catalogProvider) Facts {
	f := Facts{ID: id, Name: r.Name, Provider: pid, Context: r.window(), Output: r.Limit.Output, Inputs: clone(r.Modalities.Input), Outputs: clone(r.Modalities.Output), Temperature: r.Temperature, Efforts: p.levels(r, pid), LevelsKnown: !r.budgeted()}
	if f.Inputs != nil {
		f.Images = boolPtr(includes(f.Inputs, "image"))
	}
	f.Reasoning = r.Reasoning || len(f.Efforts) > 0
	return f
}

// Known prefers the serving provider's own catalog. For a custom relay it uses
// the catalog's shared facts, but reasoning comes from the maker before resellers
// (provider.borrowedEfforts). Empty maker levels remain authoritative.
func (c *Catalog) Known(id, base string) (*Facts, string) {
	for _, pid := range c.providerIDs(base) {
		if r, ok := c.row(pid, id); ok {
			f := rowFacts(id, pid, r, c.providers[pid])
			if key, mark, matched := c.match(id); matched {
				shared := c.aggregates[key]
				f.SharedContext, f.SharedOutput = shared.Context, shared.Output
				if mark > 0 {
					f.SharedContext = mark
				}
				f.InferredImages, f.InferredReasoning = shared.InferredImages, shared.Reasoning
			}
			return &f, "https://models.dev/api.json#" + pid
		}
	}
	key, mark, ok := c.match(id)
	f := Facts{ID: id}
	if ok {
		f = c.aggregates[key]
		f.ID = id
	}
	if mark > 0 {
		f.Context = mark
	}
	// The source maker is identified by its catalog's canonical maker/model id
	// or its own non-plan vendor id, never by a relay's display name.
	if e, known := c.borrowedEfforts(id, 0); known {
		f.Efforts = e
		f.LevelsKnown = true
	}
	if !ok && !f.LevelsKnown {
		return nil, ""
	}
	return &f, "https://models.dev/api.json"
}
