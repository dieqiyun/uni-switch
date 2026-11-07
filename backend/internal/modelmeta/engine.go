package modelmeta

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Batch struct {
	CacheDirectory  string            `json:"cacheDirectory"`
	FallbackCache   string            `json:"fallbackCache,omitempty"`
	BaseURL         string            `json:"baseUrl"`
	ProviderContext int64             `json:"providerContext,omitempty"`
	Models          []json.RawMessage `json:"models"`
	SameAs          map[string]string `json:"sameAs,omitempty"`
}

// ResolveBatch is the sole metadata merge boundary. Caller-supplied resolved
// profiles and historical official profiles are deliberately never trusted.
func (b Batch) Resolve(c *Catalog) ([]Resolved, error) {
	if len(b.Models) > 500 {
		return nil, fmt.Errorf("too many models")
	}
	out := make([]Resolved, 0, len(b.Models))
	for _, raw := range b.Models {
		var m map[string]any
		d := json.NewDecoder(strings.NewReader(string(raw)))
		d.UseNumber()
		if err := d.Decode(&m); err != nil {
			return nil, err
		}
		id := text(m["id"])
		if id == "" || len(id) > 256 {
			return nil, fmt.Errorf("invalid model id")
		}
		v := Facts{ID: id}
		if source := field(m, "metadata", "vendor"); source != nil {
			encoded, _ := json.Marshal(source)
			if err := json.Unmarshal(encoded, &v); err != nil {
				return nil, err
			}
			v.ID = id
		} else {
			// Legacy saved observations have no raw row. Retain only metadata fields
			// already normalized in the old wire format; never revive its name guesses
			// or generic "tools/structured/parallel are supported" defaults.
			p := object(m["profile"])
			v.Context = number(p["contextWindow"])
			v.Output = number(p["maxOutputTokens"])
			v.Efforts = stringsOf(p["reasoningEfforts"])
			if v.Efforts == nil {
				v.Efforts = stringsOf(m["reasoningEfforts"])
			}
			v.Inputs = Kinds(stringsOf(p["inputModalities"]))
			v.Outputs = Kinds(stringsOf(p["outputModalities"]))
			if x, ok := field(m, "capabilities", "imageInput").(bool); ok {
				v.Images = boolPtr(x)
			}
		}
		p := object(m["profileOverrides"])
		prefs := Preferences{Context: number(p["contextWindow"]), Output: number(p["maxOutputTokens"]), ProviderContext: b.ProviderContext, Efforts: stringsOf(p["reasoningEfforts"])}
		if prefs.Context == 0 {
			prefs.Context = number(m["contextWindow"])
		}
		if x, ok := field(m, "capabilityOverrides", "imageInput").(bool); ok {
			prefs.Vision = boolPtr(x)
		}
		if x, ok := p["reasoning"].(bool); ok && !x {
			prefs.Reasonless = true
		}
		known, source := c.Known(id, b.BaseURL)
		// Explicit same-as is consulted only for an id not listed as itself, before
		// rename guesses. Bound chains to prevent a settings alias cycle.
		if !c.listed[bare(id)] && !c.canonical[bare(id)] {
			seen := map[string]bool{}
			alias := bare(id)
			for depth := 0; depth < 8; depth++ {
				next := bare(strings.TrimSpace(b.SameAs[alias]))
				if next == "" || seen[next] || next == alias {
					break
				}
				seen[alias] = true
				alias = next
				if k, s := c.Known(alias, b.BaseURL); k != nil {
					known, source = k, s
					break
				}
			}
		}
		r := Resolve(v, known, prefs, source)
		r.BaseURL = b.BaseURL
		r.ProviderContext = b.ProviderContext
		if value, ok := field(m, "capabilityOverrides", "parallelToolCalls").(bool); ok {
			r.Capabilities["parallelToolCalls"] = value
			r.Sources["parallelToolCalls"] = "user:transport"
		}
		// Previously normalized protocol settings are migration input, not raw
		// supplier field guesses. Preserve them independently from metadata.
		legacy := object(m["profile"])
		for _, key := range []string{"thinkingFormat", "thinkingBetweenTools", "forcedToolChoice"} {
			if value, ok := legacy[key]; ok && value != nil {
				r.Profile[key] = value
			}
		}
		if _, exists := r.Profile["endpoints"]; !exists {
			if endpoints := object(legacy["endpoints"]); endpoints != nil {
				r.Profile["endpoints"] = endpoints
			}
		}
		if endpoints := object(p["endpoints"]); endpoints != nil {
			merged := map[string]any{}
			if old := object(r.Profile["endpoints"]); old != nil {
				for key, value := range old {
					merged[key] = value
				}
			}
			for _, key := range []string{"messages", "chatCompletions", "responses"} {
				if value, ok := endpoints[key].(bool); ok {
					merged[key] = value
					r.Sources["endpoints"] = "user:transport"
				}
			}
			if len(merged) > 0 {
				r.Profile["endpoints"] = merged
			}
		}
		// Request encoding is separate from catalog capability declarations. It is
		// projected for the existing bridge, and never invents tool/JSON support.
		for key, value := range Compatibility(id, b.BaseURL) {
			if _, exists := r.Profile[key]; !exists {
				r.Profile[key] = value
			}
		}
		for _, key := range []string{"thinkingFormat", "thinkingBetweenTools", "forcedToolChoice", "samplingParameters", "toolCalls", "structuredOutput", "thinkingAlwaysOn", "defaultEffort"} {
			if value, ok := p[key]; ok && value != nil {
				r.Profile[key] = value
				r.Sources[key] = "user:model"
			}
		}
		if prefs.Reasonless {
			r.Profile["thinkingAlwaysOn"] = false
			delete(r.Profile, "defaultEffort")
			r.Profile["thinkingFormat"] = "none"
		}
		out = append(out, r)
	}
	return out, nil
}
