package provider

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// OMLXPreset is the oMLX preset's id.
const OMLXPreset = "omlx"

// oMLX (0.7.1.dev1, jundot/omlx#4315; #1641) serves decision models —
// Clef, Clef-Flash and OpenJev — at POST /v1/systemone, System One's
// questions and answers. Its /v1/models lists them among its chat models
// with nothing to tell them apart, under the alias the user gave one; its
// /v1/models/status, asked with the same key, says each model's
// model_type, "decision" for these (found by the checkpoint's files, so
// the folder's name needn't say it), beside the id and its alias. That list
// is the provider's decision models, as OpenRouter's is (listsDecisions),
// and a model it doesn't name stays a chat model.
func (p Provider) omlxDecides() bool {
	return p.Preset == OMLXPreset && p.Decide != "" && !p.DecideOnly()
}

// omlxDecisions is where an oMLX server says each model's type.
func omlxDecisions(base string) string {
	return strings.TrimRight(base, "/") + "/models/status"
}

// listedOMLXDecide are the decision models in oMLX's /v1/models/status,
// each by the name /v1/models gives it (its alias, if it has one) and with
// the window oMLX serves it with. One the user hid in oMLX is left out, as
// /v1/models leaves it out. Clef and OpenJev both read images.
func listedOMLXDecide(b []byte) ([]catalog.Model, error) {
	var out struct {
		Models *[]struct {
			ID               string `json:"id"`
			Alias            string `json:"model_alias"`
			ModelType        string `json:"model_type"`
			EngineType       string `json:"engine_type"`
			Hidden           bool   `json:"is_hidden"`
			MaxContextWindow int    `json:"max_context_window"`
			ContextLength    int    `json:"model_context_length"`
		} `json:"models"`
	}
	if err := json.Unmarshal(b, &out); err != nil || out.Models == nil {
		return nil, fmt.Errorf("not oMLX's model status")
	}
	ms := []catalog.Model{}
	for _, m := range *out.Models {
		if m.ID == "" || m.Hidden || m.ModelType != "decision" && m.EngineType != "decision" {
			continue
		}
		id := m.ID
		if m.Alias != "" {
			id = m.Alias
		}
		images := true
		ctx := m.MaxContextWindow
		if ctx <= 0 {
			ctx = m.ContextLength
		}
		ms = append(ms, catalog.Model{ID: id, Name: id, Context: ctx, ImageInput: &images, Images: true})
	}
	return ms, nil
}
