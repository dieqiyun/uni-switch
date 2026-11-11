package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A header typed in lower case, as a browser's devtools show it, is sent
// once and is the user's: a relay that checks the client (ps.air-outer.com
// answers "unauthorized client detected" to any User-Agent but Claude
// Code's) used to get Go's User-Agent first and the user's beside it, on
// the model list, the endpoint tests and the protocol detection (#1487,
// Dudung1018). The server is HTTP/1.1, as that relay is.
func TestUserHeadersInLowerCaseAreSentOnce(t *testing.T) {
	const ua, auth = "claude-cli/2.1.0 (external, cli)", "Bearer relay-own"
	for _, entry := range []string{"list", "endpoints", "detect"} {
		t.Run(entry, func(t *testing.T) {
			detectHome(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var uas, auths []string
				for k, v := range r.Header {
					switch http.CanonicalHeaderKey(k) {
					case "User-Agent":
						uas = append(uas, v...)
					case "Authorization":
						auths = append(auths, v...)
					}
				}
				if len(uas) != 1 || uas[0] != ua || len(auths) != 1 || auths[0] != auth {
					t.Logf("%s %s: User-Agent %q, Authorization %q", r.Method, r.URL.Path, uas, auths)
					http.Error(w, `{"error":{"message":"unauthorized client detected"}}`, http.StatusUnauthorized)
					return
				}
				if r.Method == http.MethodGet {
					_, _ = w.Write([]byte(`{"data":[{"id":"claude-opus-5"}]}`))
					return
				}
				_, _ = w.Write([]byte(`{}`))
			}))
			defer srv.Close()
			p := Provider{ID: "relay", Key: "sk-test", Anthropic: srv.URL,
				Models:  []string{"claude-opus-5"},
				Headers: map[string]string{"user-agent": ua, "authorization": auth}}
			ctx := context.Background()
			var results []Result
			switch entry {
			case "list":
				if _, err := p.Fetch(ctx); err != nil {
					t.Fatalf("model list: %v", err)
				}
				return
			case "endpoints":
				results = p.Test(ctx)
			case "detect":
				detected, err := p.Detect(ctx, srv.URL, "claude-opus-5")
				if err != nil {
					t.Fatal(err)
				}
				for _, d := range detected {
					if d.Protocol == Anthropic {
						results = append(results, d.Result)
					}
				}
			}
			if len(results) == 0 {
				t.Fatal("nothing was asked")
			}
			for _, r := range results {
				if !r.OK {
					t.Errorf("%s: %+v", r.Protocol, r)
				}
			}
		})
	}
}
