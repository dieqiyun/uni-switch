package catalog

import "testing"

// canonicalCatalog is models.dev's api.json as fetched on 2026-10-10, the
// entries #1520 is about byte for byte: DeepSeek lists V4.1 Flash as
// deepseek-flash (and two deprecated ids), each canonically
// deepseek/deepseek-v4.1-flash, and no deepseek-v4.1-flash of its own.
// Resellers list variants under a model's canonical: Baseten's dearer
// DeepSeek V4.1 Flash Fast, Neuralwatt's qwen-3.8-27b and its cheaper
// -flex, OpenCode's mimo-v2.6-flash-free at $0. NVIDIA lists its own
// llama-3.1-nemotron-ultra-253b-v1 at $0. Alibaba Cloud (China) lists
// deepseek-v4.1-flash by that id, at a price of its own.
const canonicalCatalog = `
{"deepseek":{"id":"deepseek","models":{
"deepseek-flash":{"id":"deepseek-flash","name":"DeepSeek V4.1 Flash","description":"DeepSeek V4.1 Flash model for reasoning and agentic coding","family":"deepseek-flash","attachment":true,"reasoning":true,"reasoning_options":[{"type":"toggle"},{"type":"effort","values":["low","high","max"]}],"tool_call":true,"interleaved":{"field":"reasoning_content"},"structured_output":true,"temperature":true,"knowledge":"2025-05","release_date":"2026-09-10","last_updated":"2026-09-10","modalities":{"input":["text","image"],"output":["text"]},"open_weights":true,"limit":{"context":1000000,"output":393216},"cost":{"input":0.15,"output":0.6,"reasoning":0.6,"cache_read":0.003},"canonical_model_id":"deepseek/deepseek-v4.1-flash"},
"deepseek-v4-flash":{"id":"deepseek-v4-flash","name":"DeepSeek V4 Flash","description":"DeepSeek V4.1 Flash model for reasoning and agentic coding","family":"deepseek-flash","attachment":true,"reasoning":true,"reasoning_options":[{"type":"toggle"},{"type":"effort","values":["low","high","max"]}],"tool_call":true,"interleaved":{"field":"reasoning_content"},"structured_output":true,"temperature":true,"knowledge":"2025-05","release_date":"2026-09-10","last_updated":"2026-09-10","modalities":{"input":["text","image"],"output":["text"]},"open_weights":true,"limit":{"context":1000000,"output":393216},"status":"deprecated","cost":{"input":0.15,"output":0.6,"reasoning":0.6,"cache_read":0.003},"canonical_model_id":"deepseek/deepseek-v4.1-flash"},
"deepseek-v4-flash-vision-exp":{"id":"deepseek-v4-flash-vision-exp","name":"DeepSeek V4 Flash Vision Exp","description":"DeepSeek V4.1 Flash model for reasoning and agentic coding","family":"deepseek-flash","attachment":true,"reasoning":true,"reasoning_options":[{"type":"toggle"},{"type":"effort","values":["low","high","max"]}],"tool_call":true,"interleaved":{"field":"reasoning_content"},"structured_output":true,"temperature":true,"knowledge":"2025-05","release_date":"2026-09-10","last_updated":"2026-09-10","modalities":{"input":["text","image"],"output":["text"]},"open_weights":true,"limit":{"context":1000000,"output":393216},"status":"deprecated","cost":{"input":0.15,"output":0.6,"reasoning":0.6,"cache_read":0.003},"canonical_model_id":"deepseek/deepseek-v4.1-flash"},
"deepseek-v4-pro":{"id":"deepseek-v4-pro","name":"DeepSeek V4 Pro","description":"DeepSeek V4 Pro snapshot with million-token context and support for thinking and non-thinking modes","family":"deepseek-thinking","attachment":false,"reasoning":true,"reasoning_options":[{"type":"toggle"},{"type":"effort","values":["low","high","max"]}],"tool_call":true,"interleaved":{"field":"reasoning_content"},"structured_output":true,"temperature":true,"release_date":"2026-08-12","last_updated":"2026-08-22","modalities":{"input":["text"],"output":["text"]},"open_weights":true,"limit":{"context":1000000,"output":393216},"cost":{"input":0.66,"output":1.98,"reasoning":1.98,"cache_read":0.022},"canonical_model_id":"deepseek/deepseek-v4-pro-0813"}}},
"baseten":{"id":"baseten","models":{
"deepseek-ai/DeepSeek-V4.1-Flash-Fast":{"id":"deepseek-ai/DeepSeek-V4.1-Flash-Fast","name":"DeepSeek V4.1 Flash Fast","description":"Fast DeepSeek model for efficient chat, coding help, and agent loops","family":"deepseek-flash","attachment":true,"reasoning":true,"reasoning_options":[{"type":"effort","values":["none","low","high","max"]}],"tool_call":true,"interleaved":{"field":"reasoning_content"},"structured_output":true,"temperature":false,"knowledge":"2025-05","release_date":"2026-09-10","last_updated":"2026-09-10","modalities":{"input":["text","image"],"output":["text"]},"open_weights":true,"limit":{"context":1048576,"output":32768},"cost":{"input":0.6,"output":2.4},"canonical_model_id":"deepseek/deepseek-v4.1-flash"}}},
"neuralwatt":{"id":"neuralwatt","models":{
"qwen-3.8-27b":{"id":"qwen-3.8-27b","name":"Qwen3.8 27B","description":"Dense 27B vision-language model for coding, agent tasks, and image and video understanding","family":"qwen","attachment":true,"reasoning":true,"reasoning_options":[{"type":"effort","values":["none","low","medium","xhigh"]},{"type":"budget_tokens"}],"tool_call":true,"interleaved":true,"structured_output":true,"temperature":true,"release_date":"2026-08-14","last_updated":"2026-08-14","modalities":{"input":["text","image"],"output":["text"]},"open_weights":true,"limit":{"context":262128,"output":131072},"cost":{"input":0.45,"output":3.2,"cache_read":0.25},"canonical_model_id":"alibaba/qwen3.8-27b"},
"qwen-3.8-27b-flex":{"id":"qwen-3.8-27b-flex","name":"Qwen3.8 27B Flex","description":"Dense 27B vision-language model for coding, agent tasks, and image and video understanding","family":"qwen","attachment":true,"reasoning":true,"reasoning_options":[{"type":"effort","values":["none","low","medium","xhigh"]},{"type":"budget_tokens"}],"tool_call":true,"interleaved":true,"structured_output":true,"temperature":true,"release_date":"2026-08-14","last_updated":"2026-08-14","modalities":{"input":["text","image"],"output":["text"]},"open_weights":true,"limit":{"context":262128,"output":131072},"cost":{"input":0.2925,"output":2.08,"cache_read":0.1625},"canonical_model_id":"alibaba/qwen3.8-27b"}}},
"opencode":{"id":"opencode","models":{
"mimo-v2.6-flash-free":{"id":"mimo-v2.6-flash-free","name":"MiMo-V2.6-Flash Free","description":"MiMo Flash model for multimodal coding agents and long-context automation","family":"mimo","attachment":true,"reasoning":true,"reasoning_options":[],"tool_call":true,"interleaved":{"field":"reasoning_content"},"temperature":true,"release_date":"2026-09-22","last_updated":"2026-09-22","modalities":{"input":["text","image","audio","video"],"output":["text"]},"open_weights":true,"limit":{"context":200000,"output":32000},"cost":{"input":0,"output":0,"cache_read":0},"canonical_model_id":"xiaomi/mimo-v2.6-flash"}}},
"xiaomi":{"id":"xiaomi","models":{
"mimo-v2.6-flash":{"id":"mimo-v2.6-flash","name":"MiMo-V2.6-Flash","description":"MiMo Flash model for multimodal coding agents and long-context automation","family":"mimo","attachment":true,"reasoning":true,"reasoning_options":[{"type":"toggle"}],"tool_call":true,"interleaved":{"field":"reasoning_content"},"temperature":true,"release_date":"2026-09-22","last_updated":"2026-09-22","modalities":{"input":["text","image","audio","video"],"output":["text"]},"open_weights":true,"limit":{"context":1048576,"output":131072},"cost":{"input":0.14,"output":0.28,"cache_read":0.0028}}}},
"nvidia":{"id":"nvidia","models":{
"nvidia/llama-3.1-nemotron-ultra-253b-v1":{"id":"nvidia/llama-3.1-nemotron-ultra-253b-v1","name":"Llama 3.1 Nemotron Ultra 253B","description":"Flagship Nemotron model for high-throughput reasoning and complex agents","family":"nemotron","attachment":false,"reasoning":true,"reasoning_options":[{"type":"toggle"}],"tool_call":true,"temperature":true,"release_date":"2025-04-07","last_updated":"2025-04-07","modalities":{"input":["text"],"output":["text"]},"open_weights":true,"limit":{"context":128000,"output":16384},"cost":{"input":0,"output":0},"canonical_model_id":"nvidia/llama-3.1-nemotron-ultra-253b"}}},
"alibaba-cn":{"id":"alibaba-cn","models":{
"deepseek-v4.1-flash":{"id":"deepseek-v4.1-flash","name":"DeepSeek V4.1 Flash","description":"DeepSeek V4.1 Flash model for reasoning and agentic coding","family":"deepseek-flash","attachment":true,"reasoning":true,"reasoning_options":[{"type":"toggle"},{"type":"effort","values":["low","high","max"]}],"tool_call":true,"interleaved":{"field":"reasoning_content"},"structured_output":true,"temperature":true,"knowledge":"2025-05","release_date":"2026-09-10","last_updated":"2026-09-10","modalities":{"input":["text","image"],"output":["text"]},"open_weights":true,"limit":{"context":1000000,"output":384000},"cost":{"input":0.29754,"output":1.19015,"cache_read":0.01488},"canonical_model_id":"deepseek/deepseek-v4.1-flash"}}}}`

// A model its vendor lists only under another id, naming it by its
// canonical_model_id, is priced as that entry (#1520, liuweifeng):
// WorkBuddy serves deepseek-v4.1-flash, which DeepSeek sells as
// deepseek-flash at $0.15/$0.6/$0.003.
func TestPricedByCanonicalID(t *testing.T) {
	writeCatalog(t, canonicalCatalog)

	for _, id := range []string{"deepseek-v4.1-flash", "deepseek/deepseek-v4.1-flash", "DeepSeek-V4.1-Flash", "deepseek-v4-1-flash"} {
		p, ok := PricedBy([]string{"anthropic", "deepseek"}, id)
		if !ok || p.Input != 0.15 || p.Output != 0.6 || p.CacheRead != 0.003 {
			t.Errorf("PricedBy(%q) = %+v, %v, want deepseek-flash's 0.15/0.6/0.003", id, p, ok)
		}
	}
	if p, ok := PricedBy([]string{"deepseek"}, "deepseek-v4-pro-0813"); !ok || p.Input != 0.66 {
		t.Errorf("deepseek-v4-pro-0813 = %+v, %v, want deepseek-v4-pro's 0.66", p, ok)
	}
}

// A canonical id is asked last, and only of the vendor that makes the
// model: an id a vendor lists keeps its own price, and a reseller's variant
// listed under a model's canonical prices nothing but itself.
func TestPricedByCanonicalIDKeepsOtherPrices(t *testing.T) {
	writeCatalog(t, canonicalCatalog)

	// DeepSeek's own ids keep their own prices, whatever their canonical
	if p, ok := PricedBy([]string{"deepseek"}, "deepseek-v4-pro"); !ok || p.Input != 0.66 {
		t.Errorf("deepseek-v4-pro = %+v, %v", p, ok)
	}
	if p, ok := PricedBy([]string{"deepseek"}, "deepseek-flash"); !ok || p.Input != 0.15 {
		t.Errorf("deepseek-flash = %+v, %v", p, ok)
	}
	// a vendor listing the id as given is asked before any canonical one
	if p, ok := PricedBy([]string{"deepseek", "alibaba-cn"}, "deepseek-v4.1-flash"); !ok || p.Input != 0.29754 {
		t.Errorf("deepseek, alibaba-cn = %+v, %v, want alibaba-cn's listed 0.29754", p, ok)
	}
	// a reseller's variants: Fast at $0.6, -flex beside $0.45, free at $0
	for _, c := range []struct{ provider, id string }{
		{"baseten", "deepseek-v4.1-flash"},
		{"neuralwatt", "qwen3.8-27b"},
		{"opencode", "mimo-v2.6-flash"},
	} {
		if p, ok := PricedBy([]string{c.provider}, c.id); ok {
			t.Errorf("%s %s priced %+v by a variant", c.provider, c.id, p)
		}
	}
	// a vendor's own entry at $0 names no price for the model either
	if p, ok := PricedBy([]string{"nvidia"}, "llama-3.1-nemotron-ultra-253b"); ok {
		t.Errorf("nvidia llama-3.1-nemotron-ultra-253b priced %+v by a $0 entry", p)
	}
	// and the maker's price is the one asked next, as before
	if p, ok := PricedBy([]string{"opencode", "xiaomi"}, "mimo-v2.6-flash"); !ok || p.Input == 0 {
		t.Errorf("opencode, xiaomi mimo-v2.6-flash = %+v, %v, want Xiaomi's price", p, ok)
	}
	if p, ok := PricedBy([]string{"baseten", "deepseek"}, "deepseek-v4.1-flash"); !ok || p.Input != 0.15 {
		t.Errorf("baseten, deepseek = %+v, %v, want DeepSeek's 0.15", p, ok)
	}
	// nothing is anything's canonical by a part of its id
	for _, id := range []string{"deepseek-v4.1", "v4.1-flash", "deepseek-v4.1-flash-2", "kimi-k3-1", "deepseek"} {
		if p, ok := PricedBy([]string{"deepseek", "baseten", "neuralwatt", "opencode"}, id); ok {
			t.Errorf("%q priced %+v", id, p)
		}
	}
}

// Entries a vendor lists under one canonical at different prices price
// none of them, whichever the map gives first. models.dev has none such
// today without the canonical id listed too (Moonshot's kimi-k2.7-code
// beside its HighSpeed), so this one is made up in that shape.
func TestPricedByCanonicalIDOfTwoPrices(t *testing.T) {
	writeCatalog(t, `{"moonshotai":{"id":"moonshotai","models":{
"kimi-k2.7-code-highspeed":{"id":"kimi-k2.7-code-highspeed","cost":{"input":1.9,"output":8,"cache_read":0.38},"canonical_model_id":"moonshotai/kimi-k2.7-code"},
"kimi-k2.7-code-0612":{"id":"kimi-k2.7-code-0612","cost":{"input":0.95,"output":4,"cache_read":0.19},"canonical_model_id":"moonshotai/kimi-k2.7-code"}}}}`)
	for range 20 {
		if p, ok := PricedBy([]string{"moonshotai"}, "kimi-k2.7-code"); ok {
			t.Fatalf("kimi-k2.7-code priced %+v from two prices", p)
		}
	}
}
