package configuration

import (
	"github.com/dieqiyun/uni-switch/backend/internal/agent"
)

func (s *Service) Status(target string) (Object, error) {
	t, e := s.target(target)
	if e != nil {
		return nil, e
	}
	state, message := "unmanaged", "添加一组 API 配置，然后应用到此客户端。"
	if t.Active != nil {
		state = "applied"
		message = "配置已写入。请完全退出客户端，再重新打开以使用新配置。"
	}
	var applied Object
	if t.Summary != nil {
		if e = parse(*t.Summary, &applied); e != nil {
			return nil, e
		}
	}
	var active *agent.Provider
	if t.Active != nil {
		p, e := s.provider(*t.Active)
		if e != nil {
			return nil, e
		}
		p = p.ForTarget(target)
		active = &p
	}
	for _, f := range t.Baseline {
		changed, e := agent.Changed(f, active)
		if e != nil {
			state = "error"
			message = e.Error()
			break
		}
		if changed {
			state = "external_change"
			message = "API 配置已被其他工具修改。点击「使用」可确认覆盖；确认前不会修改现有文件。"
			break
		}
	}
	if state == "applied" && target != "codex" {
		p := active
		if applied != nil {
			v := agent.Provider{Summary: applied}
			p = &v
		}
		if p != nil {
			changed, e := agent.ClaudeNeedsUpdate(t.Baseline, target, *p, s.Cache)
			if e != nil {
				return nil, e
			}
			if changed {
				state = "saved_changes"
				message = "Claude 模型映射或接入地址需要更新。点击「更新配置」，然后完全退出并重新打开客户端。"
			}
		}
	}
	if state == "applied" && t.Active != nil {
		p, e := s.provider(*t.Active)
		if e == nil && (t.Source == nil || *t.Source != p.Signature()) {
			files, e := s.Plan(target, t, p)
			if e != nil {
				state = "error"
				message = e.Error()
			} else {
				for _, f := range files {
					var content *string
					for _, old := range t.Baseline {
						if old.Path == f.Path {
							content = old.Expected
						}
					}
					a, e := agent.Projection(f, content)
					if e != nil {
						return nil, e
					}
					b, e := agent.Projection(f, f.Expected)
					if e != nil {
						return nil, e
					}
					if encode(a) != encode(b) {
						state = "saved_changes"
						message = "此配置已编辑。点击重新应用，让客户端使用保存后的内容。"
						break
					}
				}
			}
		}
	}
	var model, effort any
	if target == "codex" && t.Active != nil && state == "applied" {
		if v, e := agent.Selection(t.Directory); e == nil {
			doc := obj(v)
			model = doc["model"]
			effort = doc["reasoningEffort"]
		}
	}
	if t.Active != nil && t.Manual {
		state = "external_change"
		message = "配置已手动保存。重新应用供应商将覆盖相关设置，请先确认。"
	}
	specs, e := agent.Specifications(target, t.Directory, false, false)
	if e != nil {
		return nil, e
	}
	paths := []string{}
	for _, f := range specs {
		paths = append(paths, f.Path)
	}
	return Object{"target": target, "directory": t.Directory, "files": paths, "activeProviderId": t.Active, "state": state, "canRestore": t.Baseline != nil, "message": message, "appliedModel": applied["model"], "configuredModel": model, "configuredReasoningEffort": effort, "configurationRevision": t.Revision}, nil
}
