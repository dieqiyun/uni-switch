package configuration

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/google/uuid"
)

type approval struct {
	Token, Target, ProviderID, Provider, State string
	Files                                      []edit.Change
	Created                                    time.Time
}

var approvals = struct {
	sync.Mutex
	Values map[string]approval
}{Values: map[string]approval{}}

func changedApproval() error {
	return fail("overwrite_confirmation_changed", "配置或供应商在确认期间发生变化，或确认已过期。此次没有写入，请重新确认覆盖。")
}
func (s *Service) Plan(target string, t Target, p agent.Provider) ([]agent.ManagedFile, error) {
	if p.Converted(target) {
		for _, v := range arr(p.Options()["conversionDisabledTargets"]) {
			if v == target {
				return nil, fail("conversion_required", "此供应商需要先开启协议转换再使用")
			}
		}
	}
	p = p.ForTarget(target)
	routed := agent.Clone(p)
	converted := p.Converted(target)
	if converted {
		if s.Route == nil {
			return nil, fail("bridge_unavailable", "本地协议转换服务尚未启动，请重启 uni-switch")
		}
		base := fmt.Sprintf("http://127.0.0.1:%d/v1/%s", s.Route.Port, p.ID())
		if target != "codex" {
			base = fmt.Sprintf("http://127.0.0.1:%d/claude/%s/%s", s.Route.Port, target, p.ID())
		}
		routed.Summary["baseUrl"] = base
		routed.Summary["authMode"] = "bearer"
		routed.APIKey = s.Route.Token
		if target == "claude_desktop" {
			models, e := p.Models(s.Cache, true)
			if e != nil {
				return nil, e
			}
			rows := arr(routed.Options()["models"])
			for _, m := range models {
				if m.ID == p.Model() {
					routed.Summary["model"] = gateway.DesktopID(m)
				}
				for _, v := range rows {
					r := obj(v)
					if r["id"] == m.ID {
						r["id"] = gateway.DesktopID(m)
					}
				}
			}
		}
	}
	files, e := agent.PlanPrimary(target, t.Directory, routed, s.Cache)
	if e != nil {
		return nil, e
	}
	if target == "claude_desktop" && converted {
		models, e := p.Models(s.Cache, true)
		if e != nil {
			return nil, e
		}
		if len(models) > 200 {
			return nil, fail("desktop_model_limit", "Claude 桌面端最多启用 200 个模型，请减少勾选数量")
		}
		ids := []string{}
		for _, m := range models {
			ids = append(ids, m.ID)
		}
		inference, e := agent.DesktopModels(ids, true, models)
		if e != nil {
			return nil, e
		}
		for i, f := range files {
			if has(f.Keys, "inferenceModels") {
				doc, e := agent.Parse("json", f.Expected)
				if e != nil {
					return nil, e
				}
				doc["inferenceModels"] = inference
				files[i].Expected = ptr(pretty(doc))
			}
		}
	}
	if !converted {
		bridge, e := s.bridge(target)
		if e != nil {
			return nil, e
		}
		if bridge != nil {
			for i, f := range files {
				key := ""
				if target == "codex" && f.Format == "toml" {
					key = "web_search"
				}
				if target == "claude_cli" && filepath.Base(f.Path) == "settings.json" {
					key = "env.ENABLE_TOOL_SEARCH"
				}
				if key == "" {
					continue
				}
				for _, old := range t.Baseline {
					if old.Path == f.Path {
						original, e := agent.Parse(f.Format, old.Original)
						if e != nil {
							return nil, e
						}
						v := agent.Get(original, key)
						after, e := agent.Mutate(f.Format, f.Expected, []agent.Mutation{{Path: strings.Split(key, "."), Value: v, Remove: v == nil}})
						if e != nil {
							return nil, e
						}
						files[i].Expected = &after
						if !has(files[i].Keys, key) {
							files[i].Keys = append(files[i].Keys, key)
						}
					}
				}
			}
		}
	}
	path := BridgePath(s.Directory, target)
	before, e := edit.Read(path)
	if e != nil {
		return nil, e
	}
	var after *string
	if converted {
		after = ptr(encode(p))
	}
	files = append(files, agent.ManagedFile{Path: path, Format: "json", Keys: []string{"summary", "api_key", "balance_access_token"}, Original: before, Expected: after})
	return files, nil
}
func pretty(v any) string { return prettyJSON(v) }
func has(a []string, k string) bool {
	for _, v := range a {
		if v == k {
			return true
		}
	}
	return false
}
func (s *Service) Apply(target string, p agent.Provider, update *agent.Provider, preserve bool, source *string, approval *approval) error {
	t, e := s.target(target)
	if e != nil {
		return e
	}
	if approval == nil && t.Baseline != nil && t.Manual {
		return fail("external_change", "配置已手动保存，请确认后再应用供应商")
	}
	if source == nil {
		source = ptr(p.Signature())
	}
	p = p.ForTarget(target)
	var active *agent.Provider
	if t.Active != nil {
		v, e := s.provider(*t.Active)
		if e != nil {
			return e
		}
		v = v.ForTarget(target)
		active = &v
	}
	files, e := s.Plan(target, t, p)
	if e != nil {
		return e
	}
	if approval != nil {
		if len(files) != len(approval.Files) {
			return changedApproval()
		}
		for i, f := range files {
			if f.Path != approval.Files[i].Path || !agent.Same(f.Original, approval.Files[i].Before) {
				return changedApproval()
			}
		}
	}
	for i, f := range files {
		for _, old := range t.Baseline {
			if old.Path == f.Path {
				for _, k := range []string{"web_search", agent.DisplayKey} {
					if has(old.Keys, k) && !has(files[i].Keys, k) {
						files[i].Keys = append(files[i].Keys, k)
					}
				}
				if approval == nil {
					changed, e := agent.Changed(old, active)
					if e != nil {
						return e
					}
					if changed {
						return fail("external_change", "当前 API 配置已被其他工具修改。请点击「使用」确认是否覆盖；确认前不会修改现有文件。")
					}
				}
			}
		}
	}
	keep := active != nil && active.Model() == p.Model()
	changes := []edit.Change{}
	for _, f := range files {
		after := f.Expected
		if preserve && target == "codex" && f.Format == "toml" {
			after, e = agent.Preserve(f, p, keep)
			if e != nil {
				return e
			}
		}
		if !agent.Same(f.Original, after) {
			changes = append(changes, edit.Change{Path: f.Path, Before: f.Original, After: after})
		}
	}
	for i, f := range files {
		for _, old := range t.Baseline {
			if old.Path == f.Path {
				snap := f
				snap.Expected = f.Original
				snap.Original = old.Original
				snap.Keys = old.Keys
				restored, e := agent.Restore(snap)
				if e != nil {
					return e
				}
				files[i].Original = restored.After
			}
		}
	}
	return s.Execute(Pending{Target: target, Changes: changes, Baseline: files, Active: ptr(p.ID()), ProviderUpdate: update, AppliedSummary: p.Summary, AcceptedSource: source})
}
func (s *Service) Prepare(target, id string) (Object, error) {
	approvals.Lock()
	delete(approvals.Values, s.Directory)
	approvals.Unlock()
	if e := s.Recover(); e != nil {
		return nil, e
	}
	p, e := s.provider(id)
	if e != nil {
		return nil, e
	}
	t, e := s.target(target)
	if e != nil {
		return nil, e
	}
	if t.Baseline == nil {
		return nil, changedApproval()
	}
	files, e := s.Plan(target, t, p)
	if e != nil {
		return nil, e
	}
	token := uuid.NewString()
	a := approval{Token: token, Target: target, ProviderID: id, Provider: encode(p), State: encode(t), Created: time.Now()}
	paths := []string{}
	for _, f := range files {
		a.Files = append(a.Files, edit.Change{Path: f.Path, Before: f.Original})
		if f.Path != BridgePath(s.Directory, target) && !agent.Same(f.Original, f.Expected) {
			paths = append(paths, f.Path)
		}
	}
	approvals.Lock()
	approvals.Values[s.Directory] = a
	approvals.Unlock()
	return Object{"token": token, "target": target, "providerId": id, "directory": t.Directory, "files": paths}, nil
}
func (s *Service) Confirm(target, id, token string) error {
	approvals.Lock()
	a, ok := approvals.Values[s.Directory]
	delete(approvals.Values, s.Directory)
	approvals.Unlock()
	if !ok || a.Token != token || a.Target != target || a.ProviderID != id || time.Since(a.Created) > 300*time.Second {
		return changedApproval()
	}
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
	if encode(p) != a.Provider || encode(t) != a.State {
		return changedApproval()
	}
	for _, f := range a.Files {
		v, e := edit.Read(f.Path)
		if e != nil {
			return e
		}
		if !agent.Same(v, f.Before) {
			return changedApproval()
		}
	}
	return s.Apply(target, p, nil, false, nil, &a)
}
func (s *Service) Restore(target string, update *agent.Provider) error {
	if e := s.Recover(); e != nil {
		return e
	}
	t, e := s.target(target)
	if e != nil {
		return e
	}
	if t.Baseline == nil {
		return fail("nothing_to_restore", "此客户端尚未由 uni-switch 接管")
	}
	var p *agent.Provider
	if t.Active != nil {
		v, e := s.provider(*t.Active)
		if e != nil {
			return e
		}
		v = v.ForTarget(target)
		p = &v
	}
	changes := []edit.Change{}
	for _, f := range t.Baseline {
		changed, e := agent.Changed(f, p)
		if e != nil {
			return e
		}
		if !changed {
			f.Expected, e = edit.Read(f.Path)
			if e != nil {
				return e
			}
		}
		c, e := agent.Restore(f)
		if e != nil {
			return e
		}
		changes = append(changes, c)
	}
	return s.Execute(Pending{Target: target, Changes: changes, ProviderUpdate: update})
}
