package agent

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

type ClientInput struct {
	Home           string          `json:"home"`
	Local          string          `json:"local"`
	Explicit       bool            `json:"explicit"`
	Client         string          `json:"client"`
	Directory      string          `json:"directory"`
	DefaultBinding bool            `json:"defaultBinding"`
	Editor         bool            `json:"editor"`
	Provider       *Provider       `json:"provider"`
	File           *ManagedFile    `json:"file"`
	Files          []ManagedFile   `json:"files"`
	Content        *string         `json:"content"`
	Path           string          `json:"path"`
	Format         string          `json:"format"`
	Catalog        Object          `json:"catalog"`
	Config         Object          `json:"config"`
	Cache          string          `json:"cacheDirectory"`
	Enabled        bool            `json:"enabled"`
	Keep           bool            `json:"keep"`
	IDs            []string        `json:"ids"`
	Mapped         bool            `json:"mapped"`
	Models         []gateway.Model `json:"models"`
	Protocol       string          `json:"protocol"`
	Previous       *edit.Change    `json:"previous"`
}

func ClientRPC(method string, data []byte) (any, error) {
	var in ClientInput
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil {
		return nil, fail("core_request", "客户端配置参数无效")
	}
	switch method {
	case "agent.client.inspect":
		return Inspect(in.Client, in.Directory, in.Explicit)
	case "agent.client.discoveryDefault":
		return DiscoveryDefault(in.Client, in.Home, in.Local), nil
	case "agent.client.defaultDirectory":
		return DefaultDirectory(in.Client)
	case "agent.client.specifications":
		return Specifications(in.Client, in.Directory, in.DefaultBinding, in.Editor)
	case "agent.client.read":
		return edit.Read(in.Path)
	case "agent.client.checkPath":
		return true, CheckPath(in.Path)
	case "agent.client.revision":
		return Revision(in.Path, in.Content), nil
	case "agent.client.validate":
		if in.Content == nil {
			return nil, fail("core_request", "缺少文件内容")
		}
		return true, Validate(in.Format, *in.Content)
	case "agent.client.plan":
		if in.Provider == nil {
			return nil, fail("core_request", "缺少供应商")
		}
		return PlanPrimary(in.Client, in.Directory, *in.Provider, in.Cache)
	case "agent.client.catalog":
		if in.Provider == nil {
			return nil, fail("core_request", "缺少供应商")
		}
		return ModelCatalog(*in.Provider, in.Cache)
	case "agent.client.repairCatalog":
		if in.Provider == nil {
			return nil, fail("core_request", "缺少供应商")
		}
		e := RepairCatalog(in.Catalog, *in.Provider, in.Cache)
		return in.Catalog, e
	case "agent.client.repairReasoning":
		e := RepairReasoning(in.Catalog)
		return in.Catalog, e
	case "agent.client.display":
		raw := in.Content
		doc, e := Parse("toml", raw)
		if e != nil {
			return nil, e
		}
		m, e := displayMutation(doc)
		if e != nil {
			return nil, e
		}
		return Mutate("toml", raw, []Mutation{m})
	case "agent.client.mergeRepair":
		doc, e := Parse("toml", in.Content)
		if e != nil {
			return nil, e
		}
		raw := in.File.Expected
		repaired, e := Parse("toml", raw)
		if e != nil {
			return nil, e
		}
		m := Mutation{Path: strings.Split(DisplayKey, "."), Value: Get(repaired, DisplayKey)}
		ms := []Mutation{m, {Path: []string{"model_catalog_json"}, Value: Get(repaired, "model_catalog_json")}}
		_ = doc
		return Mutate("toml", in.Content, ms)
	case "agent.client.repairFiles":
		if in.Provider == nil {
			return nil, fail("core_request", "缺少供应商")
		}
		return RepairFiles(in.Directory, *in.Provider, in.Cache, nil, nil)
	case "agent.client.fastFiles":
		if in.Provider == nil {
			return nil, fail("core_request", "缺少供应商")
		}
		dir := ""
		for _, f := range in.Files {
			if f.Format == "toml" {
				dir = filepath.Dir(f.Path)
			}
		}
		if dir == "" {
			return nil, fail("invalid_state", "当前 Codex 配置缺少管理记录，请重新使用供应商后重试")
		}
		return RepairFiles(dir, *in.Provider, in.Cache, in.Files, &in.Enabled)
	case "agent.client.preserve":
		if in.File == nil || in.Provider == nil {
			return nil, fail("core_request", "缺少快照")
		}
		return Preserve(*in.File, *in.Provider, in.Keep)
	case "agent.client.projection":
		if in.File == nil {
			return nil, fail("core_request", "缺少快照")
		}
		return Projection(*in.File, in.Content)
	case "agent.client.changed":
		if in.File == nil {
			return nil, fail("core_request", "缺少快照")
		}
		return Changed(*in.File, in.Provider)
	case "agent.client.restore":
		if in.File == nil {
			return nil, fail("core_request", "缺少快照")
		}
		return Restore(*in.File)
	case "agent.client.desktopModels":
		return DesktopModels(in.IDs, in.Mapped, in.Models)
	case "agent.client.claudeNeedsUpdate":
		if in.Provider == nil {
			return nil, fail("core_request", "缺少供应商")
		}
		return ClaudeNeedsUpdate(in.Files, in.Client, *in.Provider, in.Cache)
	case "agent.client.claudeBaseUpdate":
		for _, f := range in.Files {
			doc, e := Parse("json", f.Expected)
			if e != nil {
				return nil, e
			}
			for _, key := range []string{"env.ANTHROPIC_BASE_URL", "inferenceGatewayBaseUrl"} {
				base := text(Get(doc, key))
				if has(f.Keys, key) && strings.TrimSuffix(strings.TrimRight(base, "/"), "/v1") != base {
					return true, nil
				}
			}
		}
		return false, nil
	case "agent.client.selection":
		return Selection(in.Directory)
	case "agent.client.preferences":
		return Preferences(in.Client, in.Directory)
	case "agent.client.forTarget":
		if in.Provider == nil {
			return nil, fail("core_request", "缺少供应商")
		}
		return in.Provider.ForTarget(in.Client), nil
	case "agent.client.signature":
		if in.Provider == nil {
			return nil, fail("core_request", "缺少供应商")
		}
		return in.Provider.Signature(), nil
	case "agent.client.extraPlan":
		if in.Provider == nil {
			return nil, fail("core_request", "缺少供应商")
		}
		p := *in.Provider
		models, e := p.Models(in.Cache, true)
		if e != nil {
			return nil, e
		}
		v := Input{Client: in.Client, Name: text(p.Summary["name"]), Model: p.Model(), Models: models, Protocol: in.Protocol, Upstream: p.Upstream(), BaseURL: text(p.Summary["baseUrl"]), APIKey: p.APIKey, AuthMode: text(p.Summary["authMode"]), Previous: in.Previous}
		for _, f := range in.Files {
			if e := CheckPath(f.Path); e != nil {
				return nil, e
			}
			raw, e := edit.Read(f.Path)
			if e != nil {
				return nil, e
			}
			v.Files = append(v.Files, File{Path: f.Path, Format: f.Format, Content: raw})
		}
		return Build(v)
	}
	return nil, fail("core_method", "未知客户端操作")
}
