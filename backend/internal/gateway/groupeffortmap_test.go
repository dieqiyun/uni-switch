package gateway

import (
	"encoding/json"

	"testing"
)

// levelsListed are the reasoning levels /v1/models' body lists for id.
func levelsListed(t *testing.T, body []byte, id string) []string {
	t.Helper()
	var list struct {
		Data []struct {
			ID     string `json:"id"`
			Levels []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	for _, m := range list.Data {
		if m.ID == id {
			var out []string
			for _, l := range m.Levels {
				out = append(out, l.Effort)
			}
			return out
		}
	}
	t.Fatalf("%s not listed: %s", id, body)
	return nil
}
