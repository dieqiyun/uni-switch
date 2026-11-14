package provider

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// Ollama (0.35.0 and later, 0.40.5 for its cloud models) serves decision
// models (Nimble, Clef, Tev …) at POST /v1/systemone, System One's
// questions and answers, on the local server and at ollama.com with an API
// key alike (docs.ollama.com/api/systemone; Nanka on Discord). Its
// /v1/models lists them among its chat models with nothing to tell them
// apart; its /api/tags, asked with the same key, gives each model its
// capabilities, "decision" for these (and "vision" for one that reads
// images, as Clef does). That list is the provider's decision models, as
// oMLX's and MLX-Serve's are (listsDecisions), and a model it doesn't mark
// stays a chat model. An Ollama older than its System One lists no
// capabilities, so none.
func (p Provider) ollamaDecides() bool {
	return (p.Preset == "ollama" || p.Preset == "ollama-cloud") && p.Decide != "" && !p.DecideOnly()
}

// ollamaCloudContext is the window Ollama's docs give a System One model
// in its cloud.
const ollamaCloudContext = 65_536

// ollamaDecisions is where an Ollama server lists its models with their
// capabilities: /api/tags at the root its /v1 is under.
func ollamaDecisions(base string) string {
	return strings.TrimSuffix(strings.TrimRight(base, "/"), "/v1") + "/api/tags"
}

// listedOllamaDecide are the decision models in Ollama's /api/tags, by the
// name /v1/models gives them too, with the window the list gives (a GGUF's
// details.context_length), else, in Ollama's cloud (at ollama.com, or a
// local server's cloud model, which names its remote_host), the cloud's.
func listedOllamaDecide(base string, b []byte) ([]catalog.Model, error) {
	var out struct {
		Models *[]struct {
			Name         string   `json:"name"`
			Model        string   `json:"model"`
			RemoteHost   string   `json:"remote_host"`
			Capabilities []string `json:"capabilities"`
			Details      struct {
				ContextLength int `json:"context_length"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := json.Unmarshal(b, &out); err != nil || out.Models == nil {
		return nil, fmt.Errorf("not Ollama's model list")
	}
	cloud := HostOf(base) == "ollama.com"
	ms := []catalog.Model{}
	for _, m := range *out.Models {
		id := m.Model
		if id == "" {
			id = m.Name
		}
		if id == "" || !slices.Contains(m.Capabilities, "decision") {
			continue
		}
		images := slices.Contains(m.Capabilities, "vision")
		ctx := m.Details.ContextLength
		if ctx <= 0 && (cloud || m.RemoteHost != "") {
			ctx = ollamaCloudContext
		}
		ms = append(ms, catalog.Model{ID: id, Name: id, Context: ctx, ImageInput: &images, Images: images})
	}
	return ms, nil
}
