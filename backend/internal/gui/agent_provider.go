package gui

import (
	"encoding/json"
	"net/http"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
)

func agentProviderAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/agents/provider-models/{id}", func(w http.ResponseWriter, r *http.Request) {
		a, err := agent.Find(r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, a.ProviderModels(r.URL.Query().Get("provider")))
	})
	mux.HandleFunc("POST /api/agents/use-provider/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Provider, Model string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(w, err)
			return
		}
		a, err := agent.Find(r.PathValue("id"))
		if err == nil {
			err = a.UseProvider(in.Provider, in.Model)
		}
		if err != nil {
			fail(w, err)
			return
		}
		s := state()
		if a.Notice != nil {
			s.Notice = a.Notice()
		}
		writeJSON(w, s)
	})
}
