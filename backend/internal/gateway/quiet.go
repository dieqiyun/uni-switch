package gateway

// A routing group reasons when any member does (provider.groupEntries), so
// an agent asks a group for reasoning that a member of it may not think
// at all: the group's own model is a member's, and its levels are the ones
// the members share. Asking such a member anyway is what #950 is about:
// some vendors turn a reasoning ask away with a 400 on a model that can't
// think, where uni-switch knows from the model's own list that it can't.
//
// The gateway sends such a member no reasoning ask: the effort is for the
// members that take it, and a member known not to think is sent the
// request as one that asked for none. Which member goes first is not
// changed: the group routes as it did, and only the body changes. A model
// nothing speaks for isn't touched — it is sent the effort the agent asked
// for, as #597 leaves it — and neither is a request to a model of its own,
// outside a group. Nor is a member the user fixed at an effort (candidate
// effort, #189): that is the user saying this model does think, over a
// catalog that may be wrong about it — the Trae plugin's own model, whose
// plugin gave no capabilities, is what #950 was reported about — so the
// user's word goes as it is, and uni-switch's default doesn't override it.

import (
	"bytes"
	"encoding/json"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// quietTo is whether pid/model is known not to think: uni-switch has a word on
// it, and that word says no reasoning. Like blindTo, it asks the served
// entry, so a provider kept unlisted still answers.
func quietTo(pid, model string) bool {
	e, ok := provider.ServedEntryOf(pid + "/" + model)
	return ok && e.Quiet()
}

// withoutReasoningAsk removes the fields that ask a model to think, in the
// agent's own protocol, keeping every other field: what a member known not
// to think is sent (#950). Chat's reasoning_effort and reasoning,
// Responses' reasoning (with reasoning_effort, which some relays take),
// Anthropic's thinking when it asks to think — thinking disabled is no
// ask and stays — and output_config's effort alone, the rest of
// output_config kept, and Gemini's generationConfig.thinkingConfig when it
// asks to think. The body goes as it is when it asked for nothing.
func withoutReasoningAsk(proto provider.Protocol, body []byte) []byte {
	return withoutReasoningFields(proto, body, false)
}

// withoutAnyReasoning removes every field about reasoning, in the agent's own
// protocol: withoutReasoningAsk's, and those turning it off as well —
// Anthropic's thinking of any type, Gemini's thinkingConfig whatever it
// says, and Chat's thinking switches (Kimi's thinking, Qwen's
// enable_thinking), and the sealed reasoning Responses' include asks
// for, which OpenAI refuses of a model that doesn't reason. It is what a
// model the user said takes no reasoning ask is sent
// (provider.SetModelReasonless; yuexian7 on Discord: the vendor answers
// reasoning_effort with 422, and the request without it works).
func withoutAnyReasoning(proto provider.Protocol, body []byte) []byte {
	return withoutReasoningFields(proto, body, true)
}

func withoutReasoningFields(proto provider.Protocol, body []byte, all bool) []byte {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var m map[string]any
	if dec.Decode(&m) != nil {
		return body
	}
	changed := false
	drop := func(k string) {
		if _, ok := m[k]; ok {
			delete(m, k)
			changed = true
		}
	}
	switch proto {
	case provider.Chat, provider.Responses:
		drop("reasoning_effort")
		drop("reasoning")
		if all {
			drop("thinking")
			drop("enable_thinking")
			if inc, ok := m["include"].([]any); ok {
				kept := inc[:0:0]
				for _, v := range inc {
					if v != "reasoning.encrypted_content" {
						kept = append(kept, v)
					}
				}
				if len(kept) < len(inc) {
					m["include"], changed = kept, true
				}
			}
		}
		// Responses' changes of effort mid-thread ask for reasoning too
		if in, ok := m["input"].([]any); ok && proto == provider.Responses {
			kept := in[:0:0]
			for _, it := range in {
				if o, ok := it.(map[string]any); !ok || o["type"] != "configuration_update" {
					kept = append(kept, it)
				}
			}
			if len(kept) < len(in) {
				m["input"], changed = kept, true
			}
		}
	case provider.Anthropic:
		if t, ok := m["thinking"].(map[string]any); ok {
			if s, _ := t["type"].(string); all || s == "enabled" || s == "adaptive" {
				drop("thinking")
			}
		} else if all {
			drop("thinking")
		}
		if oc, ok := m["output_config"].(map[string]any); ok {
			if _, ok := oc["effort"]; ok {
				delete(oc, "effort")
				changed = true
				if len(oc) == 0 {
					drop("output_config")
				}
			}
		}
	case provider.Gemini:
		// generationConfig's thinkingConfig when it asks to think (as
		// parseGemini reads it), the rest of generationConfig kept; a
		// budget of 0 is no ask and stays. The body was spelled in
		// camelCase on its way in (geminiCamel).
		if gc, ok := m["generationConfig"].(map[string]any); ok {
			if raw, err := json.Marshal(gc["thinkingConfig"]); err == nil {
				var tc gThinking
				if _, has := gc["thinkingConfig"]; has && all || json.Unmarshal(raw, &tc) == nil && tc.effort() != "" {
					delete(gc, "thinkingConfig")
					changed = true
					if len(gc) == 0 {
						drop("generationConfig")
					}
				}
			}
		}
	default:
		return body
	}
	if !changed {
		return body
	}
	// the fields the agent sent, as it wrote them: <, > and & are not
	// escaped (withFields)
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if enc.Encode(m) != nil {
		return body
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n"))
}
