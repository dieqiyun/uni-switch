package library

import "testing"

// AnySearch (lileo on Discord) answers without a key, at lower rate limits,
// and takes one as a Bearer token (anysearch-ai/anysearch-mcp-server's
// README): added without one it sends no Authorization at all, and with
// one it sends it as Bearer.
func TestAnySearchKeyIsOptional(t *testing.T) {
	for _, c := range []struct{ key, want string }{{"", ""}, {" as_sk_k ", "Bearer as_sk_k"}} {
		t.Run(c.want, func(t *testing.T) {
			sandbox(t)
			ok(t)(InstallServer("anysearch", map[string]string{"Authorization": c.key}, []string{"claude"}))
			l, err := loadLocked()
			if err != nil || len(l.MCP) != 1 {
				t.Fatalf("library: %+v %v", l, err)
			}
			s := l.MCP[0]
			if s.Name != "anysearch" || s.Transport != "http" || s.URL != "https://api.anysearch.com/mcp" {
				t.Fatalf("server: %+v", s)
			}
			if got, has := s.Headers["Authorization"]; got != c.want || has != (c.want != "") {
				t.Fatalf("Authorization %q (%v), want %q: %+v", got, has, c.want, s.Headers)
			}
		})
	}
}
