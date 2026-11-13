package gateway

// A rule that names a group in the group puts the group's whole plan
// first, in the order the group's own routing planned it (#2407): ranked
// by the group's listed members, a rotate group's own order was erased and
// every request the rule put first was served by its first member.

import (
	"encoding/json"

	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// rotateModelsUp answers with the model it was asked for, so every member
// of the nested layout is tellable apart.
func rotateModelsUp(t *testing.T, id string, models ...string) {
	t.Helper()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var q struct {
			Model string `json:"model"`
		}
		json.Unmarshal(b, &q)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ok","choices":[{"index":0,"message":{"role":"assistant","content":"served-by-`+q.Model+`"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(up.Close)
	p := provider.Provider{ID: id, Name: strings.ToUpper(id), Key: "k", Chat: up.URL + "/v1", Models: models}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
}

func servedModel(body string) string {
	if i := strings.Index(body, "served-by-"); i >= 0 {
		rest := body[i+len("served-by-"):]
		if j := strings.Index(rest, `"`); j >= 0 {
			return rest[:j] + " "
		}
	}
	return ""
}
