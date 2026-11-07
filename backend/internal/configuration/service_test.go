package configuration

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/google/uuid"
)

func fixture(t *testing.T) (*Service, agent.Provider) {
	t.Helper()
	dir := t.TempDir()
	s, e := Open(Input{Directory: dir})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.db.Close() })
	_, e = s.db.Exec("CREATE TABLE providers(id TEXT PRIMARY KEY,family TEXT,data TEXT);CREATE TABLE targets(id TEXT PRIMARY KEY,directory TEXT,active TEXT,baseline TEXT,applied_at_ms INTEGER DEFAULT 0,applied_summary TEXT,accepted_source TEXT,configuration_manual INTEGER DEFAULT 0,directory_selected INTEGER DEFAULT 0);CREATE TABLE operations(id TEXT PRIMARY KEY,status TEXT,data TEXT,created_at INTEGER);")
	if e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{"codex", "claude_cli", "claude_desktop"} {
		if _, e = s.db.Exec("INSERT INTO targets(id,directory) VALUES(?,?)", id, filepath.Join(dir, id)); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.Initialize(); e != nil {
		t.Fatal(e)
	}
	p := agent.Provider{Summary: Object{"id": "fixture", "name": "Fixture", "family": "codex", "baseUrl": "https://synthetic.invalid/v1", "model": "fixture-model", "authMode": "bearer", "reasoningEffort": nil, "codexOptions": Object{"protocol": "openai", "models": []any{}}}, APIKey: "synthetic-key"}
	if _, e = s.db.Exec("INSERT INTO providers VALUES(?,?,?)", p.ID(), "codex", encode(p)); e != nil {
		t.Fatal(e)
	}
	return s, p
}
func code(err error) string {
	var e *edit.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
func TestGoConfirmationExpiresIsConsumedAndBindsEverySnapshot(t *testing.T) {
	for _, change := range []string{"expired", "file", "provider", "target", "token"} {
		t.Run(change, func(t *testing.T) {
			s, p := fixture(t)
			if e := s.Apply("codex", p, nil, false, nil, nil); e != nil {
				t.Fatal(e)
			}
			confirmation, e := s.Prepare("codex", p.ID())
			if e != nil {
				t.Fatal(e)
			}
			token := str(confirmation["token"])
			target, _ := s.target("codex")
			path := filepath.Join(target.Directory, "config.toml")
			before, _ := edit.Read(path)
			switch change {
			case "expired":
				approvals.Lock()
				a := approvals.Values[s.Directory]
				a.Created = time.Now().Add(-301 * time.Second)
				approvals.Values[s.Directory] = a
				approvals.Unlock()
			case "file":
				_ = edit.WriteAtomic(path, ptr(*before+"# 外部修改\n"))
			case "provider":
				p.APIKey = "synthetic-changed"
				_ = s.saveProvider(p)
			case "target":
				_, _ = s.db.Exec("UPDATE targets SET applied_at_ms=applied_at_ms+1 WHERE id='codex'")
			case "token":
				token = "invalid"
			}
			expected, _ := edit.Read(path)
			if e = s.Confirm("codex", p.ID(), token); code(e) != "overwrite_confirmation_changed" {
				t.Fatalf("wrong rejection %v", e)
			}
			actual, _ := edit.Read(path)
			if !agent.Same(expected, actual) {
				t.Fatal("rejected confirmation wrote file")
			}
			if e = s.Confirm("codex", p.ID(), str(confirmation["token"])); code(e) != "overwrite_confirmation_changed" {
				t.Fatal("approval reused", e)
			}
		})
	}
}
func TestGoApplyNoopRestoreAndCrashRecoveryOwnTheirDatabaseCommit(t *testing.T) {
	s, p := fixture(t)
	target, _ := s.target("codex")
	path := filepath.Join(target.Directory, "config.toml")
	original := "# 个人配置\nmodel='personal'\n[projects.\"D:/personal\"]\ntrust_level='trusted'\n"
	if e := edit.WriteAtomic(path, &original); e != nil {
		t.Fatal(e)
	}
	if e := s.Apply("codex", p, nil, false, nil, nil); e != nil {
		t.Fatal(e)
	}
	before, _ := edit.Read(path)
	first, _ := s.target("codex")
	if e := s.Apply("codex", p, nil, false, nil, nil); e != nil {
		t.Fatal(e)
	}
	after, _ := edit.Read(path)
	second, _ := s.target("codex")
	if !agent.Same(before, after) || first.Revision != second.Revision {
		t.Fatal("no-op rewrote client")
	}
	if e := s.Restore("codex", nil); e != nil {
		t.Fatal(e)
	}
	restored, _ := edit.Read(path)
	if restored == nil || !strings.Contains(*restored, "trust_level='trusted'") || !strings.Contains(*restored, "# 个人配置") {
		t.Fatal("restore lost personal settings")
	}
	a, b := filepath.Join(s.Directory, "a.json"), filepath.Join(s.Directory, "b.json")
	pnd := Pending{Target: "codex", Changes: []edit.Change{{Path: a, Before: nil, After: ptr("a")}, {Path: b, Before: nil, After: ptr("b")}}, Active: ptr(p.ID())}
	id := uuid.NewString()
	_, e := s.db.Exec("INSERT INTO operations VALUES(?,'pending',?,1)", id, encode(pnd))
	if e != nil {
		t.Fatal(e)
	}
	_ = edit.WriteAtomic(a, ptr("a"))
	if e = s.Recover(); e != nil {
		t.Fatal(e)
	}
	v, _ := edit.Read(a)
	if v != nil {
		t.Fatal("partial write not rolled back")
	}
	id = uuid.NewString()
	_, _ = s.db.Exec("INSERT INTO operations VALUES(?,'pending',?,2)", id, encode(pnd))
	_ = edit.Apply(pnd.Changes)
	if e = s.Recover(); e != nil {
		t.Fatal(e)
	}
	target, _ = s.target("codex")
	if target.Active == nil || *target.Active != p.ID() {
		t.Fatal("complete file set did not commit state")
	}
	if e = s.Recover(); e != nil {
		t.Fatal(e)
	}
}
