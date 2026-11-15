package gui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Bringing over the providers another app (CC Switch, Alma) has set up:
// the page lists them, keys masked, and posts back the ones the user
// picked; the keys are read again here, never sent to the page.

func importFingerprint(source string, it provider.AppImport) string {
	p := it.Provider
	p.ID = "" // settle's generated ID can change when another source is added
	b, _ := json.Marshal(struct {
		Source, Ref string
		Provider    provider.Provider
	}{source, it.Ref, p})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func importAppsRoutes(mux *http.ServeMux) {
	// Opaque fingerprints let the window remember dismissed configurations
	// without keeping their credentials, endpoints or paths in localStorage.
	mux.HandleFunc("GET /api/importapps/discovery", func(rw http.ResponseWriter, r *http.Request) {
		type candidate struct {
			Fingerprint string `json:"fingerprint"`
			Source      string `json:"source"`
		}
		out := []candidate{}
		// only the apps looked in unasked: OpenCode waits for the picker
		for _, s := range provider.DiscoverSources() {
			for _, it := range s.Items {
				// Match openImportApps' default selection: disabled entries and
				// ID collisions without an add-key option need a manual choice.
				if it.Skip == "" && it.Status != "same" && it.Off == "" && (it.Status != "taken" || it.KeyOf != "" || it.Reserved) {
					out = append(out, candidate{importFingerprint(s.ID, it), s.Name})
				}
			}
		}
		writeJSON(rw, out)
	})
	mux.HandleFunc("GET /api/importapps", func(rw http.ResponseWriter, r *http.Request) {
		type item struct {
			provider.AppImport
			Fingerprint string `json:"fingerprint"`
		}
		type source struct {
			provider.AppSource
			Items []item `json:"items"`
		}
		sources := []source{}
		for _, s := range provider.ImportSources() {
			out := source{AppSource: s, Items: []item{}}
			for _, it := range s.Items {
				fingerprint := importFingerprint(s.ID, it)
				p := &it.Provider
				p.Key, p.Keys = provider.Mask(p.Key), nil
				out.Items = append(out.Items, item{it, fingerprint})
			}
			sources = append(sources, out)
		}
		writeJSON(rw, sources)
	})
	mux.HandleFunc("POST /api/importapps", func(rw http.ResponseWriter, r *http.Request) {
		var in struct{ Picks []provider.AppPick }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(rw, err)
			return
		}
		had := provider.KeysHeld()
		added, err := provider.ImportFromApps(in.Picks)
		// a key imported beside others is given the models its own list
		// has, as one added by hand is (PrefillNewKeys); those an import
		// that stopped part way added too
		provider.PrefillNewKeys(r.Context(), had)
		if err != nil {
			fail(rw, err)
			return
		}
		writeJSON(rw, struct {
			Added []string `json:"added"`
			State any      `json:"state"`
		}{added, providersState()})
	})
}
