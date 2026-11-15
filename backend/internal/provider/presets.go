package provider

import (
	"cmp"
	"slices"
)

// A preset supplies defaults for a relay, local server.

// Kind groups presets in the picker.
type Kind string

const (
	KindRelay Kind = "relay" // an aggregator / API relay reselling many vendors
	KindLocal Kind = "local" // something running on this machine
)

// PresetDef describes one preset.
type PresetDef struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Icon      string   `json:"icon"`
	Kind      Kind     `json:"kind"`
	Chat      string   `json:"chat,omitempty"`
	Responses string   `json:"responses,omitempty"`
	Anthropic string   `json:"anthropic,omitempty"`
	Decide    string   `json:"decide,omitempty"` // a decision API: the provider only routes (see decide.go)
	Catalog   string   `json:"catalog,omitempty"`
	Website   string   `json:"website,omitempty"`
	SiteHost  string   `json:"siteHost,omitempty"` // a partner's: the host its io.uni-switch.desktop/go Website sends the browser to, which the editor's link reads
	KeysURL   string   `json:"keysUrl,omitempty"`
	NoKey     bool     `json:"noKey,omitempty"`     // local servers: a key is optional
	KeyHint   string   `json:"keyHint,omitempty"`   // the key field's placeholder, when it says more than NoKey's
	Sponsored bool     `json:"sponsored,omitempty"` // shown first, with a tag
	Note      string   `json:"note,omitempty"`      // one line under the name
	Short     string   `json:"short,omitempty"`     // the add sheet's name for it, when Name is long
	Regions   []Region `json:"regions,omitempty"`   // base-URL choices (a relay's regional endpoints, a vendor's plans)
	// RegionLabel names what the Regions choose between, "Region" if unset.
	RegionLabel string `json:"regionLabel,omitempty"`
	// SplitAccounts is set on a partner whose regions are China's and
	// Global's, whose accounts don't work at each other's endpoints (Kimi
	// Code): its region picker says to pick by the account.
	SplitAccounts bool `json:"splitAccounts,omitempty"`
	// KeyPlans: each key may be on a Region of its own (KeyAccount.Plan),
	// a vendor whose plans and pay as you go are sold on keys of their own
	// that only their own endpoints take (Volcengine's Coding and Agent
	// Plans beside its pay as you go, yckonnnn on X). Not a relay's
	// regions, where one key works at all of them.
	KeyPlans bool `json:"keyPlans,omitempty"`
	// HeaderHints name optional request headers the vendor documents, which
	// the editor offers to add; their values are the user's to fill in.
	HeaderHints []string `json:"headerHints,omitempty"`
	// Only, for a plan behind a key that also buys much else, is what its
	// models' ids start with: the rest of the vendor's list is left out.
	// Models are the plan's, for when the list has none of them.
	Only   string   `json:"only,omitempty"`
	Models []string `json:"models,omitempty"`
	// NoList: the vendor has no list of models to ask for (Bedrock's
	// runtime serves no /models), so Models are its list
	NoList bool `json:"noList,omitempty"`
	// Endpoint, for a vendor reached at the user's own resource (Azure
	// OpenAI), is an example of its address: the preset gives no URL, and
	// the editor asks for the one the user's resource is at, with
	// EndpointHint under it.
	Endpoint     string `json:"endpoint,omitempty"`
	EndpointHint string `json:"endpointHint,omitempty"`
	// EndpointNeeded is what the editor says when no endpoint was given.
	EndpointNeeded string `json:"endpointNeeded,omitempty"`
	// Hosts: a vendor serving other makers' models as well as its own
	// (Groq, Ollama Cloud, Azure), whose list is no maker's word on theirs
	Hosts bool `json:"-"`
	// Embeddings and Rerankers are the models of a vendor of retrieval
	// models alone (Voyage AI), which has no list to ask: served on
	// /v1/embeddings and /v1/rerank, kept out of agents' model pickers.
	// A Region's own lists stand over them.
	Embeddings []string `json:"embeddings,omitempty"`
	Rerankers  []string `json:"rerankers,omitempty"`
}

// Region is one base-URL option of a preset that offers several. The first
// is the default; picking another in the editor swaps the endpoints.
type Region struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Note string `json:"note,omitempty"`
	// ImagesOnly keeps the editor from offering dialogue endpoint tests.
	// Chat is still the base used by the OpenAI image generation API.
	ImagesOnly bool   `json:"imagesOnly,omitempty"`
	Chat       string `json:"chat,omitempty"`
	Responses  string `json:"responses,omitempty"`
	Anthropic  string `json:"anthropic,omitempty"`
	// Lists is set on a region that serves a model list although the
	// preset as a whole has none to ask for (NoList): a provider at its
	// endpoints is asked for it. KeysURL, when the region's keys are made
	// on another page than the preset's, is where its Get-a-key link goes.
	Lists   bool   `json:"lists,omitempty"`
	KeysURL string `json:"keysUrl,omitempty"`
	// Website, when the region's docs are another page than the preset's,
	// is where the editor's link goes.
	Website  string `json:"website,omitempty"`
	SiteHost string `json:"siteHost,omitempty"` // as PresetDef's
	// Catalog, for a region whose models another models.dev entry names
	// than the preset's (Tencent Cloud's pay as you go, beside its plan's
	// none), is that entry's id.
	Catalog string `json:"catalog,omitempty"`
	// Models are a plan's text models, for a region that is one beside
	// another plan or pay as you go (PresetDef.Models, for the region alone).
	// Embeddings, Rerankers, Drawers and Videos are its non-chat models, kept apart from
	// agents' model pickers.
	Models     []string `json:"models,omitempty"`
	Embeddings []string `json:"embeddings,omitempty"`
	Rerankers  []string `json:"rerankers,omitempty"`
	Drawers    []string `json:"drawers,omitempty"`
	Videos     []string `json:"videos,omitempty"`
	// Images says which of Models take images, as the vendor's own list
	// says, for a plan whose /models doesn't list its models: a model
	// it leaves out is judged as before (models.dev, the user's say).
	Images map[string]bool `json:"-"`
	// Decide is the region's decision API, for a preset that routes groups
	Decide string `json:"decide,omitempty"`
}

// WorkspaceID stands in a decision API's address for the user's Bailian
// workspace id (#647), which the editor and `workspace=` fill in.
const WorkspaceID = "{WorkspaceId}"

// presets are ordered as they appear in the picker.
var presets = []PresetDef{
	{ID: "dieqiyun", Name: "蝶祈云API", Icon: "dieqiyun", Kind: KindRelay,
		Chat: "https://cfapi.dieqiyun.top/v1", Responses: "https://cfapi.dieqiyun.top/v1", Anthropic: "https://cfapi.dieqiyun.top",
		Website: "https://www.dieqiyun.top", KeysURL: "https://www.dieqiyun.top", RegionLabel: "Service endpoint",
		Regions: []Region{
			{ID: "cfapi", Name: "Recommended endpoint", Note: "For regular users. CDN endpoint; recommended by default.",
				Chat: "https://cfapi.dieqiyun.top/v1", Responses: "https://cfapi.dieqiyun.top/v1", Anthropic: "https://cfapi.dieqiyun.top"},
			{ID: "haapi", Name: "Downstream endpoint", Note: "Los Angeles endpoint for downstream resellers. Regular users should use the recommended endpoint.",
				Chat: "https://haapi.dieqiyun.top/v1", Responses: "https://haapi.dieqiyun.top/v1", Anthropic: "https://haapi.dieqiyun.top"},
			{ID: "hw", Name: "CF CDN backup", Note: "Backup endpoint behind Cloudflare CDN.",
				Chat: "https://hw.dieqiyun.top/v1", Responses: "https://hw.dieqiyun.top/v1", Anthropic: "https://hw.dieqiyun.top"},
		}},

	// its decision models (Nimble, Clef, Tev …), served at /v1/systemone
	// since 0.35.0, are the ones its /api/tags gives the "decision"
	// capability (ollama_decide.go)
	{ID: "ollama", Name: "Ollama", Icon: "ollama", Kind: KindLocal, NoKey: true,
		Chat: "http://localhost:11434/v1", Anthropic: "http://localhost:11434",
		Decide: "http://localhost:11434/v1",
		Note:   "your local models", Website: "https://ollama.com"},
	{ID: "lmstudio", Name: "LM Studio", Icon: "lmstudio", Kind: KindLocal, NoKey: true,
		Chat: "http://localhost:1234/v1",
		Note: "local server, port 1234 by default", Website: "https://lmstudio.ai"},
	// its decision models (Clef, OpenJev), served at /v1/systemone since
	// 0.7.1, are the ones its /v1/models/status says are (omlx_decide.go)
	{ID: OMLXPreset, Name: "oMLX", Icon: "omlx", Kind: KindLocal, NoKey: true,
		Chat: "http://localhost:8000/v1", Responses: "http://localhost:8000/v1", Anthropic: "http://localhost:8000",
		Decide: "http://localhost:8000/v1",
		Note:   "local server, port 8000 by default", Website: "https://omlx.ai"},
	// its decision models (Clef, Clef-Flash, Kev, Laya, D1), served at
	// /v1/systemone, are marked by capabilities in /v1/models (mlxserve_decide.go)
	{ID: MLXServePreset, Name: "MLX-Serve", Icon: "mlx-serve", Kind: KindLocal, NoKey: true,
		Chat: "http://localhost:11234/v1", Responses: "http://localhost:11234/v1", Anthropic: "http://localhost:11234",
		Decide: "http://localhost:11234/v1",
		Note:   "local server, port 11234 by default", Website: "https://github.com/ddalcu/mlx-serve"},
}

// Presets lists every preset, sponsored ones first within their kind.
func Presets() []PresetDef {
	out := make([]PresetDef, 0, len(presets))
	for _, p := range presets {
		if p.Sponsored {
			out = append(out, p)
		}
	}
	for _, p := range presets {
		if !p.Sponsored {
			out = append(out, p)
		}
	}
	return out
}

// Preset finds a built-in preset.
func Preset(id string) *PresetDef {
	return builtinPreset(id)
}

// builtinPreset is the preset of that id built into the application.
func builtinPreset(id string) *PresetDef {
	for i := range presets {
		if presets[i].ID == id {
			return &presets[i]
		}
	}
	return nil
}

// FromPreset builds a provider from a preset; the caller adds the key.
func FromPreset(id string) (Provider, error) {
	pr := Preset(id)
	if pr == nil {
		return Provider{}, errorf("no preset %q — uni-switch presets lists them", id)
	}
	p := Provider{
		ID: pr.ID, Name: pr.Name, Icon: pr.Icon, Preset: pr.ID,
		Chat: pr.Chat, Responses: pr.Responses, Anthropic: pr.Anthropic, Decide: pr.Decide,
		Catalog: pr.Catalog, Website: pr.Website, KeysURL: pr.KeysURL,
	}
	return p, nil
}

// atRegionOf puts the provider at a region of its preset pr: its
// endpoints, and its key page, docs and catalog where it has its own.
func (p *Provider) atRegionOf(pr *PresetDef, r Region) {
	p.Chat, p.Responses, p.Anthropic, p.Decide = r.Chat, r.Responses, r.Anthropic, cmp.Or(r.Decide, pr.Decide)
	p.KeysURL = cmp.Or(r.KeysURL, p.KeysURL)
	p.Website = cmp.Or(r.Website, p.Website)
	p.Catalog = cmp.Or(r.Catalog, pr.Catalog)
}

// IconForCatalog names the logo of the vendor behind a models.dev
// provider id, or "" when no preset covers it.
func IconForCatalog(catalogID string) string {
	for _, p := range presets {
		if p.Catalog == catalogID || slices.ContainsFunc(p.Regions, func(r Region) bool { return r.Catalog == catalogID }) {
			return p.Icon
		}
	}
	return ""
}
