package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// FlatModelIDs fits pickers which split provider/model at every slash. A
// catalog-level collision check also protects literal tildes and escaped IDs.
func FlatModelIDs(models []Model) map[string]string {
	ids := []string{}
	occupied := map[string]bool{}
	for _, m := range models {
		if !occupied[m.ID] {
			ids = append(ids, m.ID)
			occupied[m.ID] = true
		}
	}
	sort.Strings(ids)
	out := map[string]string{}
	for _, id := range ids {
		flat := strings.ReplaceAll(id, "/", "~")
		if flat != id && occupied[flat] {
			sum := sha256.Sum256([]byte(id))
			flat = "uni~" + hex.EncodeToString(sum[:])
			for occupied[flat] {
				flat = "uni~" + flat
			}
		}
		out[id] = flat
		occupied[flat] = true
	}
	return out
}
