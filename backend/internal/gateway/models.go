package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"regexp"
	"slices"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/modelmeta"
)

var claudeID = regexp.MustCompile(`^claude-(?:opus|sonnet|haiku|fable|mythos)-\d+(?:-\d+)?$`)
var datedID = regexp.MustCompile(`-\d{8}$`)
var claudeFamilyID = regexp.MustCompile(`^claude-(?:(opus|sonnet|haiku|fable|mythos)-\d+(?:[.-]\d+)?(?:-\d{8})?|\d+(?:[.-]\d+)?-(opus|sonnet|haiku|fable|mythos)(?:-\d{8})?)(?:-latest)?$`)

func bareModel(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(strings.ToLower(s), "[1m]") {
		s = strings.TrimSpace(s[:len(s)-4])
	}
	return s
}
func LegacyAlias(id string) string {
	h := sha256.Sum256([]byte(bareModel(id)))
	return "claude-sonnet-4-6-uni-" + hex.EncodeToString(h[:12])
}
func DesktopID(m Model) string {
	id := bareModel(m.ID)
	n := desktopNumber(id)
	name := strings.ToLower(id)
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.ReplaceAll(name, ".", "-")
	name = datedID.ReplaceAllString(name, "")
	levels := list(m.profile("")["reasoningEfforts"])
	if len(levels) > 0 {
		if claudeID.MatchString(name) {
			return "uni-switch-" + n + ".anthropic." + name
		}
		return "mythos-uni-switch-" + n
	}
	if claudeID.MatchString(name) {
		at := strings.LastIndex(id, "/") + 1
		return id[:at] + strings.ReplaceAll(id[at:], ".", "-")
	}
	return "anthropic/uni-switch-" + n
}

func desktopNumber(id string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return fmt.Sprintf("%010d", h.Sum64()%1e10)
}

// ModelRole accepts real Claude names and plain tier selectors. Arbitrary
// strings containing a tier (including a stale desktop hash) are not routes.
func ModelRole(id string) string {
	id = strings.ToLower(bareModel(id))
	id = id[strings.LastIndex(id, "/")+1:]
	for _, role := range []string{"sonnet", "opus", "haiku", "fable", "mythos"} {
		if id == role {
			return role
		}
	}
	if match := claudeFamilyID.FindStringSubmatch(id); match != nil {
		return match[1] + match[2]
	}
	return ""
}
func modelRole(id string) string { return ModelRole(id) }

func alternateDesktopID(m Model) string {
	alternate := m
	levels := []string{"low"}
	if len(list(m.profile("")["reasoningEfforts"])) > 0 {
		levels = nil
	}
	alternate.Metadata = &modelmeta.Resolved{Profile: Object{"reasoningEfforts": levels}}
	return DesktopID(alternate)
}
func (c Call) enabledModels() []Model {
	var out []Model
	for _, m := range c.Models {
		if m.Enabled || m.ID == c.DefaultModel {
			out = append(out, m)
		}
	}
	for _, m := range out {
		if m.ID == c.DefaultModel {
			return out
		}
	}
	if c.DefaultModel != "" {
		out = append([]Model{{ID: c.DefaultModel, Enabled: true}}, out...)
	}
	return out
}
func (c Call) resolveModel(asked string) (Model, error) {
	name := bareModel(asked)
	ms := c.enabledModels()
	for _, m := range ms {
		if m.ID == name || bareModel(m.ID) == name {
			m.ID = bareModel(m.ID)
			return m, nil
		}
	}
	for _, m := range ms {
		if DesktopID(m) == name || alternateDesktopID(m) == name || LegacyAlias(m.ID) == name {
			m.ID = bareModel(m.ID)
			return m, nil
		}
	}
	for _, m := range c.Models {
		if bareModel(m.ID) == name {
			return Model{}, invalid("该模型已停用")
		}
	}
	if strings.HasPrefix(name, "claude-sonnet-4-6-uni-") {
		return Model{}, invalid("模型路由已失效")
	}
	if role := modelRole(name); role != "" {
		for _, m := range ms {
			mr := modelRole(m.ID)
			if mr == role || role == "fable" && mr == "opus" {
				m.ID = bareModel(m.ID)
				return m, nil
			}
		}
		for _, m := range ms {
			if m.ID == c.DefaultModel && modelRole(m.ID) == "" {
				return m, nil
			}
		}
	}
	return Model{}, invalid("模型未启用或没有对应档位")
}
func (c Call) modelList() Object {
	rows := []any{}
	for _, m := range c.enabledModels() {
		id := bareModel(m.ID)
		if c.Desktop {
			id = DesktopID(m)
		}
		if c.Source == Messages {
			row := Object{"id": id, "type": "model", "display_name": bareModel(m.ID), "created_at": "2026-01-01T00:00:00Z"}
			if m.Metadata != nil && integer(m.Metadata.Profile["contextWindow"]) >= 1_000_000 {
				row["supports_1m"] = true
			}
			rows = append(rows, row)
		} else if c.Source == Gemini {
			row := Object{"name": "models/" + id, "displayName": id, "supportedGenerationMethods": []string{"generateContent", "streamGenerateContent", "countTokens"}}
			if m.Metadata != nil {
				row["inputTokenLimit"] = m.Metadata.Profile["contextWindow"]
				row["outputTokenLimit"] = m.Metadata.PublishedOutput
			}
			rows = append(rows, row)
		} else {
			rows = append(rows, Object{"id": id, "object": "model", "owned_by": "uni-switch"})
		}
	}
	if c.Source == Gemini {
		return Object{"models": rows}
	}
	if c.Source == Messages {
		return Object{"data": rows, "has_more": false, "first_id": func() any {
			if len(rows) == 0 {
				return nil
			}
			return obj(rows[0])["id"]
		}(), "last_id": func() any {
			if len(rows) == 0 {
				return nil
			}
			return obj(rows[len(rows)-1])["id"]
		}()}
	}
	return Object{"object": "list", "data": rows}
}

// Cursor Private Inference retains capability data only when streaming,
// tools and text output are declared. Prefer the provider's native APIs;
// a Claude on Messages must stay there for its reasoning controls.
func (c Call) cursorModels() Object {
	rows := []any{}
	for _, m := range c.enabledModels() {
		profile := m.profile(c.BaseURL)
		types := []string{}
		if c.Upstream == Messages {
			types = append(types, "anthropic_messages")
		} else {
			endpoints := obj(profile["endpoints"])
			if endpoints["chatCompletions"] == true {
				types = append(types, "chat_completions")
			}
			if endpoints["responses"] == true {
				types = append(types, "responses")
			}
			if len(types) == 0 {
				// The local gateway translates all three incoming APIs. An
				// explicit upstream rejection must still not be advertised.
				if endpoints["chatCompletions"] != false {
					types = append(types, "chat_completions")
				}
				if endpoints["responses"] != false {
					types = append(types, "responses")
				}
				types = append(types, "anthropic_messages")
			}
		}
		efforts := list(profile["reasoningEfforts"])
		caps := Object{"supports_tool_use": true, "supports_streaming": true, "output_modalities": []string{"text"}, "supports_reasoning": len(efforts) > 0}
		if len(efforts) > 0 {
			caps["reasoning_effort"] = efforts
		}
		if context := integer(profile["contextWindow"]); context > 0 {
			caps["context_length"] = context
		}
		if m.Metadata != nil {
			if m.Metadata.PublishedOutput > 0 {
				caps["max_output_tokens"] = m.Metadata.PublishedOutput
			}
			if image, ok := m.Metadata.Capabilities["imageInput"].(bool); ok {
				caps["supports_vision"] = image
			}
			if m.Metadata.Capabilities["reasoning"] == true {
				caps["supports_reasoning"] = true
			}
		}
		id := bareModel(m.ID)
		if strings.HasPrefix(strings.ToLower(id[strings.LastIndex(id, "/")+1:]), "claude-") && slices.Contains(types, "anthropic_messages") {
			types = []string{"anthropic_messages"}
		}
		rows = append(rows, Object{"id": id, "object": "model", "owned_by": "uni-switch", "api_types": types, "capabilities": caps})
	}
	return Object{"object": "list", "data": rows}
}

func (c Call) museModels() Object {
	rows := []any{}
	for _, m := range c.enabledModels() {
		profile := m.profile(c.BaseURL)
		context := integer(profile["contextWindow"])
		if context <= 0 {
			context = 128000
		}
		output := 0
		if m.Metadata != nil {
			output = int(m.Metadata.PublishedOutput)
		}
		if output <= 0 {
			output = min(32000, context)
		}
		images := false
		if m.Metadata != nil {
			images = m.Metadata.Capabilities["imageInput"] == true
		}
		input := []string{"text"}
		if images {
			input = append(input, "image")
		}
		meta := Object{"name": m.ID, "family": "uni-switch", "is_hidden": false, "attachment": images, "reasoning": len(list(profile["reasoningEfforts"])) > 0, "temperature": false, "tool_call": true, "modalities": Object{"input": input, "output": []string{"text"}}, "options": Object{"include": []any{}}, "variants": Object{}, "description": m.ID + " via uni-switch", "limit": Object{"context": context, "output": output}}
		rows = append(rows, Object{"id": m.ID, "object": "model", "created": 0, "owned_by": "uni-switch", "metadata": Object{"muse-code": meta}})
	}
	return Object{"object": "list", "data": rows}
}
