package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/modelmeta"
)

// Keep the existing record schema, including unknown optional fields, while
// moving its client projections into Go. Rust supplies no client-specific rules.
type Provider struct {
	Summary      Object  `json:"summary"`
	APIKey       string  `json:"api_key"`
	BalanceToken *string `json:"balance_access_token"`
}

func (p Provider) Options() Object {
	if object(p.Summary["codexOptions"]) == nil {
		p.Summary["codexOptions"] = Object{"protocol": "openai", "models": []any{}, "repairReasoningLevels": false, "fastMode": nil, "contextWindow": nil, "autoCompactTokenLimit": nil, "modelsSyncedAt": nil, "balanceQuery": nil, "upstreamProtocol": nil, "protocolPreference": nil, "authPreference": nil}
	}
	return object(p.Summary["codexOptions"])
}
func (p Provider) ID() string    { return text(p.Summary["id"]) }
func (p Provider) Model() string { return text(p.Summary["model"]) }
func (p Provider) Upstream() string {
	o := p.Options()
	if v := text(o["upstreamProtocol"]); v != "" {
		return v
	}
	if p.Summary["family"] == "codex" {
		if o["protocol"] == "anthropic" {
			return "anthropic"
		}
		return "openai"
	}
	if o["claudeProtocol"] == "openai" {
		return "openai"
	}
	return "anthropic"
}
func (p Provider) Converted(target string) bool {
	return (target == "codex") != (p.Upstream() == "openai")
}
func Clone[T any](v T) T {
	b, _ := json.Marshal(v)
	var out T
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	_ = d.Decode(&out)
	return out
}
func (p Provider) ForTarget(target string) Provider {
	p = Clone(p)
	up := p.Upstream()
	if target == "codex" {
		p.Summary["family"] = "codex"
	} else {
		p.Summary["family"] = "claude"
	}
	o := p.Options()
	o["protocol"] = up
	delete(o, "claudeProtocol")
	if up == "openai" {
		o["claudeProtocol"] = "openai"
	} else {
		o["fastMode"] = nil
	}
	if target == "codex" && len(array(o["models"])) == 0 && up == "anthropic" {
		o["models"] = []any{Object{"id": p.Model(), "enabled": true, "contextWindow": 256000, "reasoningEfforts": []any{}}}
	}
	return p
}
func Canonical(v any) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(v)
	return strings.TrimSuffix(b.String(), "\n")
}
func (p Provider) Signature() string {
	v := Clone(p)
	for _, k := range []string{"name", "updatedAt", "modelsSyncedAt"} {
		delete(v.Summary, k)
	}
	for _, k := range []string{"modelsSyncedAt", "protocolDetectedAt", "conversionDisabledTargets"} {
		delete(v.Options(), k)
	}
	h := sha256.Sum256([]byte(Canonical(v)))
	return hex.EncodeToString(h[:])
}
func (p Provider) Resolve(cache string) ([]Object, error) {
	rows := array(p.Options()["models"])
	if len(rows) == 0 {
		rows = []any{Object{"id": p.Model(), "enabled": true}}
	}
	b := modelmeta.Batch{CacheDirectory: cache, BaseURL: text(p.Summary["baseUrl"]), ProviderContext: int64(num(p.Options()["contextWindow"]))}
	for _, v := range rows {
		raw, _ := json.Marshal(v)
		b.Models = append(b.Models, raw)
	}
	cat, _, _ := (modelmeta.Cache{Directory: cache}).Load()
	resolved, e := b.Resolve(cat)
	if e != nil {
		return nil, e
	}
	out := []Object{}
	for i, v := range rows {
		m := Clone(object(v))
		raw, _ := json.Marshal(resolved[i])
		var md Object
		_ = json.Unmarshal(raw, &md)
		m["metadata"] = md
		m["contextWindow"] = resolved[i].Profile["contextWindow"]
		m["reasoningEfforts"] = resolved[i].Profile["reasoningEfforts"]
		out = append(out, m)
	}
	return out, nil
}
func (p Provider) Models(cache string, enabled bool) ([]gateway.Model, error) {
	rows, e := p.Resolve(cache)
	if e != nil {
		return nil, e
	}
	out := []gateway.Model{}
	for _, m := range rows {
		if enabled && m["enabled"] == false && m["id"] != p.Model() {
			continue
		}
		b, _ := json.Marshal(m)
		var v gateway.Model
		if json.Unmarshal(b, &v) != nil {
			return nil, fail("go_metadata", "模型资料无效")
		}
		if m["enabled"] == nil {
			v.Enabled = true
		}
		out = append(out, v)
	}
	found := false
	for _, m := range out {
		found = found || m.ID == p.Model()
	}
	if enabled && !found {
		extra := Clone(p)
		extra.Options()["models"] = []any{Object{"id": p.Model(), "enabled": true}}
		ms, e := extra.Models(cache, false)
		if e != nil {
			return nil, e
		}
		out = append(ms, out...)
	}
	return out, nil
}
func (p Provider) IDs() []string {
	ids := []string{p.Model()}
	for _, v := range array(p.Options()["models"]) {
		m := object(v)
		id := text(m["id"])
		if m["enabled"] != false && id != "" {
			found := false
			for _, s := range ids {
				found = found || s == id
			}
			if !found {
				ids = append(ids, id)
			}
		}
	}
	return ids
}
