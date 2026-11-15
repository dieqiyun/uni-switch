package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"

	"strconv"
	"strings"
	"testing"
)

// listing is a relay that lists ids and answers each request with the
// model it was asked for, recording it.
func listing(t *testing.T, a *asked, ids ...string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			var l struct {
				Data []map[string]string `json:"data"`
			}
			for _, id := range ids {
				l.Data = append(l.Data, map[string]string{"id": id})
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(l)
			return
		}
		var b struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&b)
		a.add(b.Model)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"id":"x","model":`+strconv.Quote(b.Model)+
			`,"choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],`+
			`"usage":{"prompt_tokens":5,"completion_tokens":3}}`+"\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(s.Close)
	return s
}
