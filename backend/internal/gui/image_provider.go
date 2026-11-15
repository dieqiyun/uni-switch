package gui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

const imageProviderID = "dieqiyun-image"
const imageProviderBase = "https://cfimg.dieqiyun.top/v1"

// Recognize the dedicated setup, including rows saved before it was separated
// from the provider directory. A conflicting custom row stays visible to edit.
func imageProviderMatches(p provider.Provider, base string) bool {
	return p.ID == imageProviderID && p.Chat == base && p.Responses == "" && p.Anthropic == "" && p.Gemini == "" && len(p.Keys) == 0
}

// imageProvider reads strictly: a broken provider file is not an empty setup.
// The dedicated row never replaces the user's ordinary API provider.
func imageProvider(base string) (provider.Provider, error) {
	all, err := provider.Stored()
	if err != nil {
		return provider.Provider{}, err
	}
	for _, p := range all {
		if p.ID != imageProviderID {
			continue
		}
		if !imageProviderMatches(p, base) {
			return provider.Provider{}, errors.New("image_provider_conflict: the dedicated image provider ID is already used by another configuration")
		}
		return p, nil
	}
	return provider.Provider{ID: imageProviderID, Name: "蝶祈云生图", Icon: "dieqiyun", Chat: base, QueueLimit: 20, QueueWait: 120}, nil
}

func imageProviderStatus(p provider.Provider) map[string]any {
	models := make([]map[string]string, 0)
	for _, m := range catalog.LiveDrawers(p.ID) {
		models = append(models, map[string]string{"id": p.ID + "/" + m.ID, "name": m.Name})
	}
	return map[string]any{"configured": p.Key != "", "baseUrl": p.Chat, "models": models}
}

func imageProviderRoutes(mux *http.ServeMux, base string) {
	mux.HandleFunc("GET /api/image-provider", func(rw http.ResponseWriter, r *http.Request) {
		p, err := imageProvider(base)
		if err != nil {
			fail(rw, err)
			return
		}
		writeJSON(rw, imageProviderStatus(p))
	})
	// Only setup writers wait here; settings reads and gateway requests do not.
	// A canceled, older request cannot save over a newer key's completed setup.
	writing := make(chan struct{}, 1)
	mux.HandleFunc("POST /api/image-provider", func(rw http.ResponseWriter, r *http.Request) {
		var in struct {
			Key *string `json:"key"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 8192)).Decode(&in); err != nil {
			fail(rw, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		select {
		case writing <- struct{}{}:
			defer func() { <-writing }()
		case <-ctx.Done():
			fail(rw, ctx.Err())
			return
		}
		p, err := imageProvider(base)
		if err != nil {
			fail(rw, err)
			return
		}
		if in.Key != nil {
			p.Key = strings.TrimSpace(*in.Key)
		}
		if p.Key == "" {
			fail(rw, errors.New("image_key_required: enter an API key"))
			return
		}
		// Ask only the authenticated model list, never a paid generation request
		// or an unauthenticated public catalog that could hide a bad key.
		all, err := catalog.FetchURL(p.Via(ctx), base+"/models", p.Key, false, p.Headers)
		if err != nil {
			fail(rw, fmt.Errorf("image_models_failed: %s", strings.ReplaceAll(err.Error(), p.Key, "[redacted]")))
			return
		}
		models := slices.DeleteFunc(all, func(m catalog.Model) bool { return !m.Draws || m.Films || m.Serves != "" })
		if len(models) == 0 {
			fail(rw, errors.New("image_models_empty: this key lists no image models"))
			return
		}
		if err := ctx.Err(); err != nil {
			fail(rw, err)
			return
		}
		// Re-read after the network wait, preserving settings changed meanwhile.
		fresh, err := imageProvider(base)
		if err != nil {
			fail(rw, err)
			return
		}
		fresh.Key, fresh.Off = p.Key, false
		if err := provider.Save(fresh); err != nil {
			fail(rw, err)
			return
		}
		if err := catalog.SaveLive(fresh.ID, base, models); err != nil {
			fail(rw, err)
			return
		}
		writeJSON(rw, imageProviderStatus(fresh))
	})
}
