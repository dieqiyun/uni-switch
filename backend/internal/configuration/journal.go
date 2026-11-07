package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/google/uuid"
)

// These tags intentionally preserve installed Rust journal records.
type Pending struct {
	Target         string              `json:"target"`
	Changes        []edit.Change       `json:"changes"`
	Baseline       []agent.ManagedFile `json:"baseline"`
	Active         *string             `json:"active"`
	ProviderUpdate *agent.Provider     `json:"provider_update"`
	AppliedSummary Object              `json:"applied_summary"`
	AcceptedSource *string             `json:"accepted_source"`
}

func (s *Service) Execute(p Pending) error {
	for _, c := range p.Changes {
		if e := agent.CheckPath(c.Path); e != nil {
			return e
		}
		raw, e := edit.Read(c.Path)
		if e != nil {
			return e
		}
		if !agent.Same(raw, c.Before) {
			return fail("external_change", "配置文件刚刚被其他程序修改，请刷新后重试")
		}
	}
	id := uuid.NewString()
	backups := filepath.Join(s.Directory, "backups")
	if e := edit.PrivateDirectory(backups); e != nil {
		return e
	}
	if e := edit.WriteAtomic(filepath.Join(backups, id+".json"), ptr(encode(p))); e != nil {
		return e
	}
	if _, e := s.db.Exec("INSERT INTO operations(id,status,data,created_at) VALUES(?,'pending',?,?)", id, encode(p), time.Now().Unix()); e != nil {
		return e
	}
	e := edit.Apply(p.Changes)
	if e == nil {
		e = s.finish(id, p)
	}
	if e != nil {
		if edit.Rollback(p.Changes) != nil {
			return fail("recovery_required", "部分文件无法恢复，已保留操作备份。请关闭客户端后重新打开 uni-switch；备份位于应用数据目录的 backups 文件夹")
		}
		if _, err := s.db.Exec("UPDATE operations SET status='rolled_back',data='{}' WHERE id=?", id); err != nil {
			return err
		}
		return e
	}
	_ = s.cleanup()
	return nil
}
func (s *Service) finish(id string, p Pending) error {
	var fastID, before, after string
	if p.ProviderUpdate != nil {
		updated := *p.ProviderUpdate
		if previous, e := s.provider(updated.ID()); e == nil {
			before = previous.Signature()
			changed := encode(previous.Options()["fastMode"]) != encode(updated.Options()["fastMode"])
			previous.Options()["fastMode"] = updated.Options()["fastMode"]
			after = updated.Signature()
			if changed && previous.Signature() == after {
				fastID = updated.ID()
			}
		}
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var old int64
	if e = tx.QueryRow("SELECT applied_at_ms FROM targets WHERE id=?", p.Target).Scan(&old); e != nil {
		return e
	}
	rev := old
	for _, c := range p.Changes {
		if p.Target != "codex" || filepath.Base(c.Path) == "config.toml" || filepath.Base(c.Path) == "uni-switch-models.json" {
			rev = revision(old)
			break
		}
	}
	var baseline, summary any
	if p.Baseline != nil {
		baseline = encode(p.Baseline)
	}
	if p.AppliedSummary != nil {
		summary = encode(p.AppliedSummary)
	}
	if _, e = tx.Exec("UPDATE targets SET active=?,baseline=?,applied_at_ms=?,configuration_manual=0,applied_summary=?,accepted_source=? WHERE id=?", p.Active, baseline, rev, summary, p.AcceptedSource, p.Target); e != nil {
		return e
	}
	if p.ProviderUpdate != nil {
		p := *p.ProviderUpdate
		if _, e = tx.Exec("INSERT INTO providers(id,family,data) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET family=excluded.family,data=excluded.data", p.ID(), encode(p.Summary["family"]), encode(p)); e != nil {
			return e
		}
	}
	if fastID != "" {
		if _, e = tx.Exec("UPDATE targets SET accepted_source=? WHERE active=? AND accepted_source=? AND id!=?", after, fastID, before, p.Target); e != nil {
			return e
		}
	}
	if _, e = tx.Exec("UPDATE operations SET status='complete',data='{}' WHERE id=?", id); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Service) Recover() error {
	if e := s.recoverConfig(); e != nil {
		return e
	}
	rows, e := s.db.Query("SELECT id,data FROM operations WHERE status='pending' ORDER BY created_at,rowid")
	if e != nil {
		return e
	}
	type row struct{ id, data string }
	pending := []row{}
	for rows.Next() {
		var r row
		if e = rows.Scan(&r.id, &r.data); e != nil {
			rows.Close()
			return e
		}
		pending = append(pending, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, r := range pending {
		var p Pending
		if e = parse(r.data, &p); e != nil {
			return e
		}
		all := true
		for _, c := range p.Changes {
			if e = agent.CheckPath(c.Path); e != nil {
				return e
			}
			v, e := edit.Read(c.Path)
			if e != nil || !agent.Same(v, c.After) {
				all = false
				break
			}
		}
		if all {
			e = s.finish(r.id, p)
		} else {
			e = edit.Rollback(p.Changes)
			if e == nil {
				_, e = s.db.Exec("UPDATE operations SET status='rolled_back',data='{}' WHERE id=?", r.id)
			}
		}
		if e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) cleanup() error {
	rows, e := s.db.Query("SELECT id FROM operations WHERE status!='pending' ORDER BY created_at DESC,rowid DESC LIMIT -1 OFFSET 10")
	if e != nil {
		return e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if _, e = uuid.Parse(id); e != nil || strings.ContainsAny(id, `/\`) {
			return fail("invalid_state", "备份 ID 无效")
		}
		path := filepath.Join(s.Directory, "backups", id+".json")
		if e = os.Remove(path); e != nil && !os.IsNotExist(e) {
			return e
		}
		if _, e = s.db.Exec("DELETE FROM operations WHERE id=?", id); e != nil {
			return e
		}
	}
	return nil
}
