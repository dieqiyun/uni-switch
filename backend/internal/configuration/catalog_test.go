package configuration

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/localgateway"
)

func TestCatalogFileClientsApplyReapplyAndRestore(t *testing.T) {
	s, p := fixture(t)
	s.Route = &Route{Port: 12345, Token: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	t.Cleanup(func() { localgateway.CloseAll(context.Background()) })
	for _, c := range agent.ExtraClients {
		if c.ID == "hanako" || c.ID == "alma" || c.ID == "newmax" || c.ID == "zed" {
			continue
		} // Resource owners have dedicated tests with real DB / mock APIs and credentials.
		t.Run(c.ID, func(t *testing.T) {
			if e := s.SetDirectory(c.ID, filepath.Join(s.Directory, "client-fixtures", c.ID), "", false); e != nil {
				t.Fatal(e)
			}
			if c.ID == "kirocrew" {
				if _, e := s.db.Exec("UPDATE targets SET active=? WHERE id='codex'", p.ID()); e != nil {
					t.Fatal(e)
				}
			}
			protocol := c.Protocols[0]
			if c.ID == "zcode" || c.ID == "dsh" {
				protocol = "chat_completions"
			}
			result, e := s.ApplyClient(c.ID, p.ID(), protocol, nil)
			if e != nil {
				t.Fatal(e)
			}
			st, e := s.extra(c.ID)
			if e != nil || st.Active == nil || *st.Active != p.ID() {
				t.Fatal("not applied", e)
			}
			if c.Mode == "import" {
				if str(result["importLink"]) == "" {
					t.Fatal("import link missing")
				}
			} else {
				if len(st.Baseline) == 0 {
					t.Fatal("configuration missing")
				}
				for _, change := range st.Baseline {
					now, e := readClientResource(change.Path, st.Formats[change.Path])
					if e != nil || !sameClientResource(now, change.After, st.Formats[change.Path]) {
						t.Fatal("write did not match journal", change.Path, e)
					}
				}
			}
			if _, e = s.ApplyClient(c.ID, p.ID(), protocol, nil); e != nil {
				t.Fatal("reapply", e)
			}
			if _, e = s.RestoreClient(c.ID); e != nil {
				t.Fatal("restore", e)
			}
			for _, change := range st.Baseline {
				now, e := readClientResource(change.Path, st.Formats[change.Path])
				if e != nil || !agent.Same(now, change.Before) {
					t.Fatal("did not restore original", change.Path, e)
				}
			}
			cleared, e := s.extra(c.ID)
			if e != nil || cleared.Active != nil {
				t.Fatal("route still active", e)
			}
		})
	}
}
