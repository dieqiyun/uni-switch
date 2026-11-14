package provider

// PLUGIN-SERVED (see AGENTS.md): Factory ("factory") is a retired built-in
// subscription, served by its plugin alone,
// the retired opencode-factory-auth (provider.BuiltinRetired). Its
// sign-in, models, requests and usage are the plugin's. What is left here:
// reading the accounts signed in to the built-in, for the move onto the
// plugin (migrate_factory.go); whoami, which names an API key's account
// before the plugin keeps it (factory_key.go); droid's model table, whose
// wire per model (factoryAPIs) and pools (factoryCore) the provider named
// "factory", the plugin's, still answers by; and the API's host, which a
// moved provider shows. A fix belongs in the plugin
// (github.com/dieqiyun/uni-switch, packages/factory), with the
// mover's min raised in internal/provider/migrate_factory.go.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"
)

// Where Factory and its WorkOS sign-in are; vars so tests can point them
// elsewhere.
var (
	factoryAPI   = "https://api.factory.ai"
	factoryAPIEU = "https://api.eu.factory.ai"
)

// FactoryBaseForTest points Factory's API at api, and its EU region at eu,
// until the returned function runs. A provider built after the call uses
// them. Tests outside this package use it.
func FactoryBaseForTest(api, eu string) func() {
	oldA, oldE := factoryAPI, factoryAPIEU
	factoryAPI, factoryAPIEU = api, eu
	return func() {
		factoryAPI, factoryAPIEU = oldA, oldE
	}
}

var (
	factoryClient = &http.Client{Timeout: 30 * time.Second}
	// factoryMu holds the accounts still while the mover reads them
	factoryMu sync.Mutex
)

// factoryCreds is what uni-switch keeps of a Factory account.
type factoryCreds struct {
	Access    string `json:"accessToken"`
	Refresh   string `json:"refreshToken"`
	ExpiresAt int64  `json:"expiresAt"` // unix ms, from the token's exp
	// Org is the WorkOS organization (org_…) the token was put in, kept
	// for the record: it is never sent to Factory's API.
	Org string `json:"orgId,omitempty"`
	// Active is droid's active_organization_id: Factory's own id for the
	// org, as /api/cli/whoami answers it, sent as X-Factory-Org-Id. droid
	// asks whoami as soon as it holds a token and keeps its orgId, so its
	// requests carry it; an account with none asks before it sends.
	Active string `json:"activeOrganizationId,omitempty"`
	Email  string `json:"email,omitempty"`
	UserID string `json:"userId,omitempty"`
	Region string `json:"region,omitempty"` // "eu" for an org served from Factory's EU region
	// Prem is whoami's premBaseHostV2: an org Factory serves from a host
	// of its own, where droid sends its model requests instead.
	Prem string `json:"premBaseHost,omitempty"`
	// Key: Access is a Factory API key (fk-…), as droid takes from
	// FACTORY_API_KEY: sent as it is, never renewed, with no active org
	// (droid's comes from a sign-in) — see factory_key.go.
	Key bool `json:"apiKey,omitempty"`
}

// base is the Factory API the account's org is served from.
func (c factoryCreds) base() string {
	if c.Region == "eu" {
		return factoryAPIEU
	}
	return factoryAPI
}

type factoryLogin struct {
	Login
	creds factoryCreds
}

func factorySaved(l savedLogin) (factoryCreds, bool) {
	var c factoryCreds
	if json.Unmarshal(l.Auth, &c) != nil || c.Access == "" {
		return factoryCreds{}, false
	}
	return c, true
}

// factoryLogins is every Factory account signed in, the first in use first.
func factoryLogins() []factoryLogin {
	var out []factoryLogin
	for _, l := range sideLogins("factory", "", func(l savedLogin) bool {
		_, ok := factorySaved(l)
		return ok
	}) {
		c, _ := factorySaved(l.saved)
		a := factoryLogin{Login: l.Login, creds: c}
		a.Lapsed = l.saved.Lapsed
		out = append(out, a)
	}
	return out
}

// factoryStatus is an answer Factory or WorkOS gave with an error status.
type factoryStatus struct {
	Code int
	Msg  string
}

func (e *factoryStatus) Error() string { return e.Msg }

// factoryWho is what /api/cli/whoami tells of an account.
type factoryWho struct {
	UserID string `json:"userId"`
	OrgID  string `json:"orgId"`
	Email  string `json:"email"`
	Region string `json:"region"`
	Prem   string `json:"premBaseHostV2"`
}

// factoryWhoami asks Factory whose the token is with the headers droid's
// whoami (ZA) sends: the token, X-Factory-Whoami-Extended, and the active
// org when there is one — nothing else.
func factoryWhoami(ctx context.Context, c factoryCreds) (factoryWho, error) {
	var who factoryWho
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base()+"/api/cli/whoami", nil)
	if err != nil {
		return who, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Access)
	req.Header.Set("X-Factory-Whoami-Extended", "true")
	if c.Active != "" {
		req.Header.Set("X-Factory-Org-Id", c.Active)
	}
	res, err := factoryClient.Do(req)
	if err != nil {
		return who, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return who, &factoryStatus{res.StatusCode, "Factory: " + APIError(b, res.Status)}
	}
	return who, json.Unmarshal(b, &who)
}

// ---- models ---------------------------------------------------------------

// factoryModel is one of the models Factory serves a subscription, as
// droid's registry lists it.
type factoryModel struct {
	id, name string
	api      Protocol // the wire droid sends it on
	upstream string   // the vendor droid names in x-api-provider
	context  int
	output   int
	efforts  []string
	images   bool
}

// factoryModels are the ones droid's /model picker offers, less auto (droid
// picks it client side). Gemini's are the ones droid 0.231.0's CLI registry
// still offers (provider google); 2.5 and Gemini 3 Pro Image are
// availableInCLI false there, and stay out. They go to /api/llm/g. Gemini 4
// Argon is droid 0.241.1's (#2080), behind a Factory feature flag there.
// Haiku 5.5 and Mistral Large 4 are droid 0.240.0's; GPT-6 Astra Ultra
// Fast, new there too, stays out, as droid offers it only once the user
// has accepted Factory's high-cost terms for it.
var factoryModels = []factoryModel{
	{"claude-fable-5.1", "Fable 5.1", Anthropic, "anthropic", 867000, 128000, []string{"low", "medium", "high", "xhigh", "max"}, true},
	{"claude-fable-5", "Fable 5", Anthropic, "anthropic", 867000, 128000, []string{"low", "medium", "high", "xhigh", "max"}, true},
	{"claude-opus-5-5", "Opus 5.5", Anthropic, "anthropic", 872000, 128000, []string{"low", "medium", "high", "xhigh", "max"}, true},
	{"claude-opus-5", "Opus 5", Anthropic, "anthropic", 867000, 128000, []string{"low", "medium", "high", "xhigh", "max"}, true},
	{"claude-opus-4-8", "Opus 4.8", Anthropic, "anthropic", 867000, 128000, []string{"low", "medium", "high", "xhigh", "max"}, true},
	{"claude-sonnet-5-5", "Sonnet 5.5", Anthropic, "anthropic", 872000, 128000, []string{"low", "medium", "high", "xhigh", "max"}, true},
	{"claude-sonnet-5", "Sonnet 5", Anthropic, "anthropic", 872000, 128000, []string{"low", "medium", "high", "xhigh", "max"}, true},
	{"claude-sonnet-4-6", "Sonnet 4.6", Anthropic, "anthropic", 931000, 64000, []string{"low", "medium", "high", "max"}, true},
	{"claude-haiku-5-5", "Haiku 5.5", Anthropic, "anthropic", 872000, 128000, []string{"low", "medium", "high", "xhigh", "max"}, true},
	{"claude-haiku-4-5-20251001", "Haiku 4.5", Anthropic, "anthropic", 0, 0, []string{"low", "medium", "high"}, true},
	{"gpt-6-sol", "GPT-6 Sol", Responses, "openai", 1050000, 128000, []string{"none", "low", "medium", "high", "xhigh", "max"}, true},
	{"gpt-6-astra", "GPT-6 Astra", Responses, "openai", 1050000, 128000, []string{"low", "medium", "high", "xhigh", "max"}, true},
	{"gpt-6-luna", "GPT-6 Luna", Responses, "openai", 1050000, 128000, []string{"none", "low", "medium", "high", "xhigh", "max"}, true},
	{"gpt-5.6-sol", "GPT-5.6 Sol", Responses, "openai", 1050000, 128000, []string{"none", "low", "medium", "high", "xhigh", "max"}, true},
	{"gpt-5.6-terra", "GPT-5.6 Terra", Responses, "openai", 1050000, 128000, []string{"none", "low", "medium", "high", "xhigh", "max"}, true},
	{"gpt-5.6-luna", "GPT-5.6 Luna", Responses, "openai", 1050000, 128000, []string{"none", "low", "medium", "high", "xhigh", "max"}, true},
	{"gpt-5.5", "GPT-5.5", Responses, "openai", 1050000, 128000, []string{"low", "medium", "high", "xhigh"}, true},
	{"gpt-5.4", "GPT-5.4", Responses, "openai", 1050000, 128000, []string{"low", "medium", "high", "xhigh"}, true},
	{"gpt-5.3-codex", "GPT-5.3-Codex", Responses, "openai", 400000, 128000, []string{"low", "medium", "high", "xhigh"}, true},
	{"grok-4.7", "Grok 4.7", Responses, "xai", 500000, 63356, []string{"low", "medium", "high", "xhigh"}, true},
	{"grok-4.6", "Grok 4.6", Responses, "xai", 200000, 63356, []string{"low", "medium", "high", "xhigh"}, true},
	{"gemini-4-argon", "Gemini 4 Argon", Gemini, "google", 1000000, 65536, []string{"low", "medium", "high"}, true},
	{"gemini-3.1-pro-preview", "Gemini 3.1 Pro", Gemini, "google", 1000000, 65536, []string{"low", "medium", "high"}, true},
	{"gemini-3.8-flash", "Gemini 3.8 Flash", Gemini, "google", 1000000, 65536, []string{"low", "medium", "high"}, true},
	{"gemini-3.7-flash", "Gemini 3.7 Flash", Gemini, "google", 1000000, 65536, []string{"low", "medium", "high"}, true},
	{"gemini-3.6-flash", "Gemini 3.6 Flash", Gemini, "google", 1000000, 65536, []string{"low", "medium", "high"}, true},
	{"gemini-3.5-flash", "Gemini 3.5 Flash", Gemini, "google", 1000000, 65536, []string{"minimal", "low", "medium", "high"}, true},
	{"gemini-3-flash-preview", "Gemini 3 Flash", Gemini, "google", 1000000, 65536, []string{"minimal", "low", "medium", "high"}, true},
	// reasoning is mandatory on GLM-5.3 and GLM-5.3-Flash (Fireworks turns
	// none away): low is their least. GLM-5.2, Kimi K3 and DeepSeek V4.1
	// Flash stop thinking at none, droid's "off" (#899).
	{"glm-5.3", "GLM-5.3", Chat, "fireworks", 1040000, 131072, []string{"low", "high", "max"}, false},
	{"glm-5.3-flash", "GLM-5.3-Flash", Chat, "fireworks", 1048576, 131072, []string{"low", "high", "max"}, true},
	{"glm-5.2", "GLM-5.2", Chat, "baseten", 1040000, 131072, []string{"none", "high", "max"}, false},
	{"kimi-k3", "Kimi K3", Chat, "fireworks", 262144, 65536, []string{"none", "low", "high", "max"}, true},
	{"deepseek-v4.1-flash", "DeepSeek V4.1 Flash", Chat, "fireworks", 1040000, 131072, []string{"none", "low", "high", "max"}, true},
	{"qwen3.8-max", "Qwen3.8 Max", Chat, "fireworks", 262144, 131072, []string{"low", "medium", "xhigh"}, false},
	{"minimax-m3", "MiniMax M3", Chat, "fireworks", 512000, 64000, []string{"high"}, true},
	{"minimax-m2.7", "MiniMax M2.7", Anthropic, "fireworks", 196600, 64000, []string{"high"}, false},
	{"mistral-large-4", "Mistral Large 4", Chat, "mistral", 524288, 64000, []string{"high"}, true},
	{"mistral-medium-3.5", "Mistral Medium 3.5", Chat, "mistral", 256000, 64000, []string{"high"}, true},
	{"nemotron-3-ultra", "Nemotron 3 Ultra", Chat, "baseten", 202000, 65536, []string{"high"}, false},
}

// factoryCore is whether a model is one of the open ones Factory hosts,
// billed to Droid Core; droid's registry has every one of them as provider
// "factory", served by Fireworks, Baseten or Mistral. Gemini is Google's,
// on the standard pool with Claude, GPT and Grok.
func factoryCore(id string) bool {
	m, ok := factoryModelOf(id)
	return ok && m.upstream != "anthropic" && m.upstream != "openai" && m.upstream != "xai" && m.upstream != "google"
}

func factoryModelOf(id string) (factoryModel, bool) {
	for _, m := range factoryModels {
		if m.id == id {
			return m, true
		}
	}
	return factoryModel{}, false
}

// factoryAPIs is the API a model is served on. One droid didn't list may be
// tried on the three wires the other models use, not on Gemini's generate.
func factoryAPIs(model string) []Protocol {
	if m, ok := factoryModelOf(model); ok {
		return []Protocol{m.api}
	}
	return []Protocol{Chat, Responses, Anthropic}
}

// ---- the provider ---------------------------------------------------------
