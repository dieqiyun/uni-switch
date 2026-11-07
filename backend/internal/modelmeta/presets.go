package modelmeta

import (
	_ "embed"
	"encoding/json"
	"net/url"
	"strings"
)

// Public endpoint/catalog data extracted with scripts/modelmeta-reference from
// internal/provider/presets.go at Reference. No reference code is executed.
//
//go:embed presets.json
var presetData []byte

type endpointPreset struct {
	ID, Kind, Catalog, Chat, Responses, Anthropic string
	Hosts                                         bool
	Regions                                       []endpointPreset
}

var endpointPresets = func() []endpointPreset {
	var p []endpointPreset
	if err := json.Unmarshal(presetData, &p); err != nil {
		panic(err)
	}
	return p
}()

func catalogs(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" && !includes(out, v) {
			out = append(out, v)
		}
	}
	return out
}
func (c *Catalog) makers() []string {
	var out []string
	for _, p := range endpointPresets {
		if p.Kind != "vendor" || p.Hosts {
			continue
		}
		for _, id := range catalogs(p.Catalog) {
			if !includes(out, id) {
				out = append(out, id)
			}
		}
	}
	return out
}
func endpointMatch(base, endpoint string) bool {
	a, e := url.Parse(strings.TrimRight(base, "/"))
	b, f := url.Parse(strings.TrimRight(endpoint, "/"))
	if e != nil || f != nil || a.Hostname() == "" || b.Hostname() == "" || !strings.EqualFold(a.Host, b.Host) {
		return false
	}
	// Keep plans on the same host apart. Anthropic roots may omit /v1.
	x, y := strings.TrimRight(a.Path, "/"), strings.TrimRight(b.Path, "/")
	return x == y || x+"/v1" == y || y+"/v1" == x
}
func (c *Catalog) providerIDs(base string) []string {
	// The product stores an endpoint rather than Magpie's preset id; recover the
	// exact preset/region from its full API path, never just a shared hostname.
	for _, p := range endpointPresets {
		for _, r := range p.Regions {
			for _, u := range []string{r.Chat, r.Responses, r.Anthropic} {
				if u != "" && endpointMatch(base, u) {
					if r.Catalog != "" {
						return catalogs(r.Catalog)
					}
					return catalogs(p.Catalog)
				}
			}
		}
		for _, u := range []string{p.Chat, p.Responses, p.Anthropic} {
			if u != "" && endpointMatch(base, u) {
				return catalogs(p.Catalog)
			}
		}
	}
	return nil
}
func versionless(id string) (string, bool) {
	parts := strings.Split(bare(id), "-")
	versioned, named := false, false
	for i, p := range parts {
		v := strings.TrimPrefix(p, "v")
		if v != "" && v[0] != '.' && strings.Trim(v, "0123456789.") == "" {
			parts[i] = "#"
			versioned = true
		} else {
			named = true
		}
	}
	return strings.Join(parts, "-"), versioned && named
}
func (c *Catalog) borrowedEfforts(id string, depth int) ([]string, bool) {
	if depth > 4 {
		return nil, false
	}
	key, _, matched := c.match(id)
	if !matched {
		key = bare(id)
	}
	for _, pid := range c.makers() {
		if r, ok := c.row(pid, key); ok {
			if e := c.providers[pid].levels(r, ""); len(e) > 0 || !r.budgeted() {
				return e, true
			}
		}
	}
	if f, ok := c.aggregates[key]; ok && len(f.Efforts) > 0 {
		return clone(f.Efforts), true
	}
	shape, ok := versionless(id)
	if ok {
		var bestDate, bestPID, bestKey string
		var levels []string
		found := false
		for _, pid := range c.makers() {
			for _, key := range sortedKeys(c.providers[pid].Models) {
				r := c.providers[pid].Models[key]
				s, _ := versionless(key)
				if s != shape || r.budgeted() || !r.Reasoning && len(r.efforts("")) == 0 {
					continue
				}
				if !found || r.Released > bestDate || r.Released == bestDate && pid == bestPID && key > bestKey {
					levels = c.providers[pid].levels(r, "")
					bestDate, bestPID, bestKey, found = r.Released, pid, key, true
				}
			}
		}
		if found {
			return clone(levels), true
		}
	}
	if at := effortSuffix.FindStringIndex(id); at != nil && at[0] > 0 {
		return c.borrowedEfforts(id[:at[0]], depth+1)
	}
	return nil, false
}
