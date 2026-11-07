// Package modelmeta implements our model-metadata policy from the reviewed
// Magpie catalog/provider rules. It has no Magpie runtime dependency.
// Reference: yetone/magpie@665b9aff126e26eb5fa8931b7395feb6002185f3.
package modelmeta

import (
	"encoding/json"
	"strings"
)

const Reference = "665b9aff126e26eb5fa8931b7395feb6002185f3"
const Policy = "magpie-model-spec-v1"
const LeastWindow int64 = 2048

// Facts keeps the vendor's answer distinct from catalog guesses and user input.
type Facts struct {
	ID         string `json:"id"`
	Name       string `json:"name,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Context    int64  `json:"context,omitempty"`
	MaxContext int64  `json:"maxContext,omitempty"`
	Output     int64  `json:"output,omitempty"`
	// Shared facts remain distinct from the serving provider's own limits.
	SharedContext     int64    `json:"sharedContext,omitempty"`
	SharedOutput      int64    `json:"sharedOutput,omitempty"`
	InferredReasoning bool     `json:"inferredReasoning,omitempty"`
	Images            *bool    `json:"images"`
	InferredImages    bool     `json:"inferredImages,omitempty"`
	Inputs            []string `json:"inputs"`
	Outputs           []string `json:"outputs"`
	Efforts           []string `json:"efforts"`
	// A vendor catalog match without levels is different from no match.
	LevelsKnown   bool            `json:"levelsKnown,omitempty"`
	Reasoning     bool            `json:"reasoning,omitempty"`
	AlwaysThinks  bool            `json:"alwaysThinks,omitempty"`
	DynamicTools  bool            `json:"dynamicTools,omitempty"`
	DefaultEffort string          `json:"defaultEffort,omitempty"`
	Temperature   *bool           `json:"temperature"`
	APIs          []string        `json:"apis"`
	Codex         bool            `json:"codex,omitempty"`
	CodexEntry    json.RawMessage `json:"codexEntry,omitempty"`
}

type Preferences struct {
	Context         int64    `json:"context,omitempty"`
	ProviderContext int64    `json:"providerContext,omitempty"`
	Output          int64    `json:"output,omitempty"`
	ProviderOutput  int64    `json:"providerOutput,omitempty"`
	Vision          *bool    `json:"vision"`
	Efforts         []string `json:"efforts"`
	Reasonless      bool     `json:"reasonless,omitempty"`
}

type Resolved struct {
	BaseURL         string            `json:"baseUrl,omitempty"`
	ProviderContext int64             `json:"providerContext,omitempty"`
	Policy          string            `json:"policy"`
	Reference       string            `json:"reference"`
	Vendor          Facts             `json:"vendor"`
	Catalog         *Facts            `json:"catalog,omitempty"`
	Profile         map[string]any    `json:"profile"`
	Capabilities    map[string]any    `json:"capabilities"`
	PublishedOutput int64             `json:"publishedOutput,omitempty"`
	Sources         map[string]string `json:"sources"`
	EffortMapping   map[string]string `json:"effortMapping"`
}

func Window(n int64) int64 {
	if n < LeastWindow {
		return 0
	}
	return n
}
func OutputWithin(window, output int64) int64 {
	if window > 0 && output > window {
		return window
	}
	return output
}
func boolPtr(b bool) *bool { return &b }
func stringsOf(v any) []string {
	xs, ok := v.([]any)
	if !ok {
		return nil
	}
	out := []string{}
	for _, x := range xs {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
func number(v any) int64 {
	switch n := v.(type) {
	case json.Number:
		if i, err := n.Int64(); err == nil && i > 0 && i <= 100_000_000 {
			return i
		}
	case float64:
		if n > 0 && n <= 100_000_000 {
			return int64(n)
		}
	case int64:
		if n > 0 && n <= 100_000_000 {
			return n
		}
	}
	return 0
}
func text(v any) string           { s, _ := v.(string); return s }
func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func field(v any, path ...string) any {
	for _, p := range path {
		v = object(v)[p]
	}
	return v
}
func includes(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
func clone(xs []string) []string {
	if xs == nil {
		return nil
	}
	return append([]string{}, xs...)
}

// Kinds follows catalog.Kinds, including MIME inputs and explicit empty lists.
func Kinds(xs []string) []string {
	if xs == nil {
		return nil
	}
	out := []string{}
	for _, x := range xs {
		x = strings.ToLower(strings.TrimSpace(x))
		if typ, _, ok := strings.Cut(x, "/"); ok {
			switch typ {
			case "image", "audio", "video", "text":
				x = typ
			}
			if x == "application/pdf" {
				x = "pdf"
			}
		}
		if x != "" && !includes(out, x) {
			out = append(out, x)
		}
	}
	return out
}

// Levels retains vendor vocabulary and order, skips malformed entries, and
// removes duplicates, as catalog.levelsOf does. Unknown does not become "off".
func Levels(v any) []string {
	xs, _ := v.([]any)
	var out []string
	for _, x := range xs {
		e := text(x)
		if m := object(x); m != nil {
			e = text(m["effort"])
		}
		if e != "" && !includes(out, e) {
			out = append(out, e)
		}
	}
	return out
}

func EndpointAPIs(xs []string) []string {
	var out []string
	for _, x := range xs {
		x = strings.TrimPrefix(strings.TrimSuffix(x, "/"), "/v1")
		api := map[string]string{"/messages": "anthropic", "/responses": "responses", "/chat/completions": "chat"}[x]
		if api != "" && !includes(out, api) {
			out = append(out, api)
		}
	}
	return out
}

// ParseVendor is the metadata subset of catalog.fetchPage/codexFacts/kimiFacts.
// Fields absent from that source (e.g. generic tool/structured-output flags) are
// not inferred from a model name or our historical capability switches.
func ParseVendor(id string, m map[string]any) Facts {
	f := Facts{ID: id, Name: text(m["display_name"]), Context: number(m["context_length"]), Output: number(m["max_output_tokens"])}
	if f.Name == "" {
		f.Name = text(m["name"])
	}
	if f.Name == "" {
		f.Name = id
	}
	f.Inputs, f.Outputs = Kinds(stringsOf(field(m, "modalities", "input"))), Kinds(stringsOf(field(m, "modalities", "output")))
	if f.Inputs == nil && f.Outputs == nil {
		f.Inputs, f.Outputs = Kinds(stringsOf(field(m, "modalities", "input_modalities"))), Kinds(stringsOf(field(m, "modalities", "output_modalities")))
	}
	f.Efforts = Levels(m["supported_reasoning_levels"])
	f.APIs = EndpointAPIs(stringsOf(m["supported_endpoints"]))
	if native := EndpointAPIs(stringsOf(m["native_endpoints"])); len(native) > 0 {
		f.APIs = native
	}
	if len(f.APIs) == 0 {
		switch text(m["type_target"]) {
		case "openai":
			f.APIs = []string{"chat", "responses"}
		case "anthropic":
			f.APIs = []string{"anthropic"}
		case "gemini":
			f.APIs = []string{"chat"}
		}
	}
	_, levels := m["supported_reasoning_levels"]
	_, shell := m["shell_type"]
	_, instructions := m["base_instructions"]
	f.Codex = text(m["slug"]) != "" && levels && shell && instructions
	if f.Codex {
		f.CodexEntry, _ = json.Marshal(m)
		if f.Context == 0 {
			f.Context = number(m["context_window"])
		}
		if most := number(m["max_context_window"]); most > f.Context {
			f.MaxContext = most
		}
		if f.Inputs == nil {
			f.Inputs = Kinds(stringsOf(m["input_modalities"]))
		}
		f.LevelsKnown = true
	}
	if f.Inputs != nil {
		f.Images = boolPtr(includes(f.Inputs, "image"))
	}
	if m["supports_thinking_type"] == "only" || m["supports_thinking_type"] == nil && field(m, "reasoning", "type") == "only" {
		f.AlwaysThinks = true
	}
	if b, ok := m["supports_dynamic_tools"].(bool); ok {
		f.DynamicTools = b
	} else if b, ok := field(m, "tools", "dynamic_load").(bool); ok {
		f.DynamicTools = b
	}
	f.DefaultEffort = text(field(m, "think_efforts", "default_effort"))
	if f.DefaultEffort == "" {
		f.DefaultEffort = text(field(m, "reasoning", "effort", "default"))
	}
	return f
}

var effortOrder = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}

func withUltra(id string, efforts []string) []string {
	base := bare(id)
	if at := datedSuffix.FindStringIndex(base); at != nil {
		base = base[:at[0]]
	}
	eligible := includes([]string{"gpt-6.1-sol", "gpt-6-sol", "gpt-6-astra", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-6.1-terra"}, base)
	if !eligible || !includes(efforts, "max") || includes(efforts, "ultra") {
		return efforts
	}
	return append(clone(efforts), "ultra")
}

func effortRank(s string) int {
	switch strings.ToLower(s) {
	case "light":
		s = "low"
	case "extra_high":
		s = "xhigh"
	}
	for i, e := range effortOrder {
		if e == s {
			return i
		}
	}
	return -1
}

// NearestEffort preserves vendor names, breaks ties upward, and never chooses
// none for an enabled request: catalog.NearestEffort.
func NearestEffort(want string, levels []string) string {
	at := effortRank(want)
	if len(levels) == 0 || at < 0 || includes(levels, want) {
		return want
	}
	best, distance := want, len(effortOrder)
	for _, e := range levels {
		r := effortRank(e)
		if r < 0 || e == "none" {
			continue
		}
		d := r - at
		if d < 0 {
			d = -d
		}
		if d < distance || d == distance && r > at {
			best, distance = e, d
		}
	}
	return best
}

func keptEfforts(all, kept []string) []string {
	if len(kept) == 0 {
		return clone(all)
	}
	if len(all) == 0 {
		out := []string{}
		for _, e := range effortOrder {
			if includes(kept, e) {
				out = append(out, e)
			}
		}
		return out
	}
	var out []string
	for _, e := range all {
		if includes(kept, e) {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return clone(all)
	}
	return out
}
