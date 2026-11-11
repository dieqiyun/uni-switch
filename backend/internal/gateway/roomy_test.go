package gateway

import (
	"net/http/httptest"

	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// roomyMember saves a provider id serving model m on s, its window
// window tokens (0: not known).
func roomyMember(t *testing.T, id string, window int, s *scripted) {
	t.Helper()
	up := httptest.NewServer(s)
	t.Cleanup(up.Close)
	p := provider.Provider{ID: id, Name: strings.ToUpper(id), Key: "k", Chat: up.URL + "/v1", Models: []string{"m"}}
	if window > 0 {
		p.Contexts = map[string]int{"m": window}
	}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
}
