package configuration

import (
	"strings"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
)

func (s *Service) saveProvider(p agent.Provider) error {
	_, e := s.db.Exec("UPDATE providers SET data=? WHERE id=?", encode(p), p.ID())
	return e
}
func mergeBaseline(baseline []agent.ManagedFile, files []agent.ManagedFile, repair bool) ([]agent.ManagedFile, []edit.Change, error) {
	changes := []edit.Change{}
	for _, f := range files {
		if !agent.Same(f.Original, f.Expected) {
			changes = append(changes, edit.Change{Path: f.Path, Before: f.Original, After: f.Expected})
		}
		at := -1
		for i, old := range baseline {
			if f.Path == old.Path {
				at = i
				break
			}
		}
		if at < 0 {
			baseline = append(baseline, f)
			continue
		}
		old := baseline[at]
		snap := f
		snap.Expected = f.Original
		snap.Original = old.Original
		snap.Keys = old.Keys
		restored, e := agent.Restore(snap)
		if e != nil {
			return nil, nil, e
		}
		old.Original = restored.After
		if repair && f.Format == "toml" {
			doc, e := agent.Parse("toml", f.Expected)
			if e != nil {
				return nil, nil, e
			}
			ms := []agent.Mutation{}
			for _, k := range []string{"model_catalog_json", agent.DisplayKey} {
				ms = append(ms, agent.Mutation{Path: strings.Split(k, "."), Value: agent.Get(doc, k)})
				if !has(old.Keys, k) {
					old.Keys = append(old.Keys, k)
				}
			}
			after, e := agent.Mutate("toml", old.Expected, ms)
			if e != nil {
				return nil, nil, e
			}
			old.Expected = &after
		} else {
			old.Keys = f.Keys
			old.Expected = f.Expected
		}
		baseline[at] = old
	}
	return baseline, changes, nil
}
func (s *Service) Fast(id string, enabled bool) (Object, error) {
	if e := s.Recover(); e != nil {
		return nil, e
	}
	p, e := s.provider(id)
	if e != nil {
		return nil, e
	}
	if enabled && p.Upstream() == "anthropic" {
		return nil, fail("anthropic_fast", "Claude 协议转换暂不支持 Fast，请使用 OpenAI 协议供应商")
	}
	status, e := s.Status("codex")
	if e != nil {
		return nil, e
	}
	current := status["state"] == "applied"
	t, e := s.target("codex")
	if e != nil {
		return nil, e
	}
	applied := t.Active != nil && *t.Active == id
	before := p.Signature()
	previous := agent.Clone(p)
	p.Options()["fastMode"] = enabled
	p.Summary["updatedAt"] = time.Now().Unix()
	if applied {
		if t.Baseline == nil {
			return nil, fail("invalid_state", "当前配置缺少管理记录，请重新使用供应商后重试")
		}
		summary := agent.Clone(previous.Summary)
		if t.Summary != nil {
			if e = parse(*t.Summary, &summary); e != nil {
				return nil, e
			}
		}
		ap := agent.Clone(previous)
		ap.Summary = summary
		if enabled && ap.Upstream() == "anthropic" {
			return nil, fail("anthropic_fast", "当前 Codex 仍在使用 Claude 协议转换，请先使用 OpenAI 协议配置再开启 Fast")
		}
		ap.Options()["fastMode"] = enabled
		ap.Summary["updatedAt"] = p.Summary["updatedAt"]
		ap = ap.ForTarget("codex")
		if _, e = agent.PlanPrimary("codex", t.Directory, ap, s.Cache); e != nil {
			return nil, e
		}
		previous = previous.ForTarget("codex")
		for _, f := range t.Baseline {
			changed, e := agent.Changed(f, &previous)
			if e != nil {
				return nil, e
			}
			if changed {
				return nil, fail("external_change", "当前 API 配置或模型目录已被其他工具修改，请先处理配置冲突后重试；此次没有写入")
			}
		}
		files, e := agent.RepairFiles(t.Directory, ap, s.Cache, t.Baseline, &enabled)
		if e != nil {
			return nil, e
		}
		baseline, changes, e := mergeBaseline(t.Baseline, files, false)
		if e != nil {
			return nil, e
		}
		var source *string
		if current {
			source = ptr(p.Signature())
		}
		if e = s.Execute(Pending{Target: "codex", Changes: changes, Baseline: baseline, Active: t.Active, ProviderUpdate: &p, AppliedSummary: ap.Summary, AcceptedSource: source}); e != nil {
			return nil, e
		}
	} else {
		tx, e := s.db.Begin()
		if e != nil {
			return nil, e
		}
		defer tx.Rollback()
		if _, e = tx.Exec("UPDATE providers SET data=? WHERE id=?", encode(p), id); e != nil {
			return nil, e
		}
		if _, e = tx.Exec("UPDATE targets SET accepted_source=? WHERE active=? AND accepted_source=?", p.Signature(), id, before); e != nil {
			return nil, e
		}
		if e = tx.Commit(); e != nil {
			return nil, e
		}
	}
	return Object{"provider": p.Summary, "applied": applied}, nil
}
func (s *Service) Repair(id string) (Object, error) {
	if e := s.Recover(); e != nil {
		return nil, e
	}
	p, e := s.provider(id)
	if e != nil {
		return nil, e
	}
	if p.Summary["family"] != "codex" {
		return nil, fail("wrong_family", "此修复仅适用于 Codex")
	}
	saved := agent.Clone(p)
	p.Options()["repairReasoningLevels"] = true
	p.Summary["updatedAt"] = time.Now().Unix()
	enabled := false
	for _, v := range arr(p.Options()["models"]) {
		enabled = enabled || obj(v)["enabled"] != false
	}
	if !enabled {
		p.Options()["models"] = []any{Object{"id": p.Model(), "enabled": true, "contextWindow": nil, "reasoningEfforts": []any{}}}
	}
	t, e := s.target("codex")
	if e != nil {
		return nil, e
	}
	applied := t.Active != nil && *t.Active == id
	if !applied {
		if e = s.saveProvider(p); e != nil {
			return nil, e
		}
	} else {
		if t.Baseline == nil {
			return nil, fail("invalid_state", "当前配置缺少管理记录，请重新应用后重试")
		}
		if _, e = agent.PlanPrimary("codex", t.Directory, saved, s.Cache); e != nil {
			return nil, e
		}
		for _, f := range t.Baseline {
			changed, e := agent.Changed(f, &saved)
			if e != nil {
				return nil, e
			}
			if changed {
				return nil, fail("external_change", "当前 API 配置或模型目录已被其他工具修改，请先撤回相应修改后重试；此次没有写入")
			}
		}
		files, e := agent.RepairFiles(t.Directory, p, s.Cache, nil, nil)
		if e != nil {
			return nil, e
		}
		baseline, changes, e := mergeBaseline(t.Baseline, files, true)
		if e != nil {
			return nil, e
		}
		bridge, e := s.bridge("codex")
		if e != nil {
			return nil, e
		}
		if bridge != nil && bridge.ID() == id {
			bridge.Options()["repairReasoningLevels"] = true
			bridge.Summary["updatedAt"] = p.Summary["updatedAt"]
			if len(arr(bridge.Options()["models"])) == 0 {
				bridge.Options()["models"] = p.Options()["models"]
			}
			path := BridgePath(s.Directory, "codex")
			before, e := edit.Read(path)
			if e != nil {
				return nil, e
			}
			after := ptr(encode(bridge))
			if !agent.Same(before, after) {
				changes = append(changes, edit.Change{Path: path, Before: before, After: after})
			}
			for i, f := range baseline {
				if f.Path == path {
					baseline[i].Expected = after
				}
			}
		}
		if e = s.Execute(Pending{Target: "codex", Changes: changes, Baseline: baseline, Active: t.Active, ProviderUpdate: &p, AppliedSummary: p.Summary, AcceptedSource: ptr(p.Signature())}); e != nil {
			return nil, e
		}
	}
	return Object{"provider": p.Summary, "applied": applied}, nil
}
func (s *Service) Sync(target, id string) error {
	if e := s.Recover(); e != nil {
		return e
	}
	p, e := s.provider(id)
	if e != nil {
		return e
	}
	t, e := s.target(target)
	if e != nil {
		return e
	}
	if t.Active == nil || *t.Active != id {
		return fail("target_changed", "此客户端已切换供应商，未同步")
	}
	source := p.Signature()
	var old Object
	if t.Summary != nil {
		if e = parse(*t.Summary, &old); e != nil {
			return e
		}
	}
	if old == nil {
		if b, _ := s.bridge(target); b != nil {
			old = b.Summary
		}
	}
	if old == nil {
		if prefs, e := agent.Preferences(target, t.Directory); e == nil {
			old = agent.Clone(p.Summary)
			old["model"] = prefs["model"]
			old["reasoningEffort"] = prefs["reasoningEffort"]
			o := obj(old["codexOptions"])
			for _, k := range []string{"fastMode", "contextWindow", "autoCompactTokenLimit", "models"} {
				o[k] = obj(prefs["codexOptions"])[k]
			}
			for _, m := range arr(o["models"]) {
				for _, saved := range arr(p.Options()["models"]) {
					if obj(saved)["id"] == obj(m)["id"] {
						obj(m)["capabilities"] = obj(saved)["capabilities"]
						obj(m)["capabilityOverrides"] = obj(saved)["capabilityOverrides"]
					}
				}
			}
			for _, f := range t.Baseline {
				if has(f.Keys, agent.DisplayKey) {
					o["repairReasoningLevels"] = true
				}
			}
		}
	}
	if old != nil {
		up := p.Upstream()
		disabled := p.Options()["conversionDisabledTargets"]
		p.Summary["model"] = old["model"]
		p.Summary["reasoningEffort"] = old["reasoningEffort"]
		p.Summary["codexOptions"] = old["codexOptions"]
		p.Options()["upstreamProtocol"] = up
		p.Options()["conversionDisabledTargets"] = disabled
	}
	return s.Apply(target, p, nil, true, &source, nil)
}
func (s *Service) Migrate() (bool, error) {
	t, e := s.target("codex")
	if e != nil {
		return false, e
	}
	if t.Manual || t.Active == nil || t.Baseline == nil {
		return false, nil
	}
	p, e := s.provider(*t.Active)
	if e != nil {
		return false, e
	}
	p = p.ForTarget("codex")
	if t.Summary != nil {
		if e = parse(*t.Summary, &p.Summary); e != nil {
			return false, e
		}
	}
	for _, f := range t.Baseline {
		changed, e := agent.Changed(f, &p)
		if e != nil || changed {
			return false, nil
		}
	}
	for i, f := range t.Baseline {
		if f.Format != "catalog" {
			continue
		}
		before, e := edit.Read(f.Path)
		if e != nil {
			return false, e
		}
		if !agent.Same(before, f.Expected) || before == nil {
			return false, nil
		}
		catalog, e := agent.Parse("json", before)
		if e != nil {
			return false, nil
		}
		old := encode(catalog)
		if e = agent.RepairCatalog(catalog, p, s.Cache); e != nil {
			return false, e
		}
		if old == encode(catalog) {
			return false, nil
		}
		after := ptr(pretty(catalog))
		t.Baseline[i].Expected = after
		var summary Object
		if t.Summary != nil {
			if e = parse(*t.Summary, &summary); e != nil {
				return false, e
			}
		}
		e = s.Execute(Pending{Target: "codex", Changes: []edit.Change{{Path: f.Path, Before: before, After: after}}, Baseline: t.Baseline, Active: t.Active, AppliedSummary: summary, AcceptedSource: t.Source})
		return e == nil, e
	}
	return false, nil
}
