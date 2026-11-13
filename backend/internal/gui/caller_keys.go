package gui

import (
	"cmp"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/budget"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/library"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// keyRow is a gateway key as the Gateway page lists it, with what it has
// used of its limit (#585): Used of its first, UsedAll of each (#2227),
// in the order of its limits.
type keyRow struct {
	access.Key
	Used    *budget.Status  `json:"used,omitempty"`
	UsedAll []budget.Status `json:"usedAll,omitempty"`
}

// keyModelJSON is a model a gateway key may be held to, as its picker
// lists it.
type keyModelJSON struct {
	ID           string `json:"id"` // "<provider>/<model>" or "group/<id>", what the key keeps
	Name         string `json:"name"`
	Provider     string `json:"provider,omitempty"`
	ProviderName string `json:"providerName,omitempty"`
	Group        bool   `json:"group,omitempty"`   // a routing group (Magic_zero on Discord)
	Account      bool   `json:"account,omitempty"` // a signed-in account the key may be held to (#905)
	Key          bool   `json:"key,omitempty"`     // one of a provider's keys, by its KeyID
	Plan         string `json:"plan,omitempty"`    // the account's subscription, for the menu's note
	// Decides marks a decision model (Jev, Clef…), which no agent lists:
	// a key held to some models reaches it at /v1/systemone, and another
	// uni-switch lists it, only when it is picked (Kautism on Discord)
	Decides bool `json:"decides,omitempty"`
}

// keyMCPJSON is an MCP server a gateway key may be held to, with its
// tools, as its picker lists it.
type keyMCPJSON struct {
	Name  string   `json:"name"`
	Title string   `json:"title,omitempty"`
	Tools []string `json:"tools"`
	Own   bool     `json:"own,omitempty"` // one of uni-switch's own
}

func withLimits(keys []access.Key) []keyRow {
	now := time.Now()
	out := make([]keyRow, 0, len(keys))
	for _, k := range keys {
		row := keyRow{Key: k, UsedAll: budget.All(k, now)}
		if len(row.UsedAll) > 0 {
			row.Used = &row.UsedAll[0]
		}
		out = append(out, row)
	}
	return out
}

func callerKeyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/caller-keys", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		access.MigrateLegacyLANKeyBestEffort()
		keys, err := access.List()
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, map[string]any{"keys": withLimits(keys), "accountNames": provider.AccountNames()})
	})
	// the models a key may be held to (#882), and the accounts and keys
	// (#905): the routing groups, then each provider's models, then its
	// accounts and keys; a group the key names is its with every member
	mux.HandleFunc("GET /api/caller-keys/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		out := []keyModelJSON{}
		accounts := []keyModelJSON{}
		for _, e := range provider.Catalog() {

			out = append(out, keyModelJSON{ID: e.Provider.ID + "/" + e.Model, Name: cmp.Or(e.Name, e.Model), Provider: e.Provider.ID, ProviderName: cmp.Or(e.Provider.Name, e.Provider.ID)})
		}

		// the decision models the routing groups' pickers offer, each
		// after its provider's chat models: the gateway holds a key to
		// them as to any model (serveSystemOne, and another uni-switch's list
		// of them), so a key held to some models had no way to be let use
		// one but All models (Kautism on Discord: a key held to two
		// groups, and Clef and Jev listed nowhere)
		for _, e := range provider.OfferedDeciders(nil) {
			id := e.Provider.ID + "/" + e.Model
			if slices.ContainsFunc(out, func(m keyModelJSON) bool { return strings.EqualFold(m.ID, id) }) {
				continue
			}
			m := keyModelJSON{ID: id, Name: cmp.Or(e.Name, e.Model), Provider: e.Provider.ID, ProviderName: cmp.Or(e.Provider.Name, e.Provider.ID), Decides: true}
			at := len(out)
			for i := len(out) - 1; i >= 0; i-- {
				if out[i].Provider == m.Provider {
					at = i + 1
					break
				}
			}
			out = slices.Insert(out, at, m)
		}
		for _, p := range provider.All() {
			if !p.On() {
				continue
			}
			name := cmp.Or(p.Name, p.ID)
			// an account by its stable id, kept through renames, shown by the
			// name the user gave it, else who is signed in; a key by its
			// fingerprint
			for _, a := range p.AccountIDs() {
				accounts = append(accounts, keyModelJSON{ID: p.ID + "/" + a.ID, Name: a.Shown(p), Provider: p.ID, ProviderName: name, Account: !a.Key, Key: a.Key, Plan: a.Plan})
			}
		}
		writeJSON(w, map[string]any{"models": out, "accounts": accounts})
	})
	// the MCP servers and tools a key may be held to (wakaka on Discord):
	// uni-switch's own, then the Library's, each with its tools by name
	mux.HandleFunc("GET /api/caller-keys/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		out := []keyMCPJSON{}
		for _, o := range gateway.OwnMCPTools(r.Context()) {
			m := keyMCPJSON{Name: o.Name, Title: o.Title, Tools: o.Tools, Own: true}
			if m.Tools == nil {
				m.Tools = []string{}
			}
			out = append(out, m)
		}
		for _, s := range library.KeyMCPServers() {
			m := keyMCPJSON{Name: s.Name, Title: s.Title, Tools: []string{}}
			for _, t := range s.Tools {
				m.Tools = append(m.Tools, t.Name)
			}
			out = append(out, m)
		}
		writeJSON(w, map[string]any{"servers": out})
	})
	mux.HandleFunc("POST /api/caller-keys/{action}", func(w http.ResponseWriter, r *http.Request) {
		var in access.Change
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(w, err)
			return
		}
		access.MigrateLegacyLANKeyBestEffort()
		secret, err := access.Update(r.PathValue("action"), in)
		if err != nil {
			fail(w, err)
			return
		}
		keys, err := access.List()
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, map[string]any{"keys": withLimits(keys), "accountNames": provider.AccountNames(), "secret": secret})
	})
}
