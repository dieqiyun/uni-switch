use crate::types::{
    EndpointSupport, ModelCapabilities, ModelProfile, ProviderModel, ThinkingFormat,
};
use serde_json::Value;

pub fn known(id: &str) -> ModelCapabilities {
    crate::model_registry::entry(id)
        .map(|e| e.capabilities)
        .unwrap_or_default()
}
pub fn known_profile(id: &str) -> ModelProfile {
    crate::model_registry::entry(id)
        .map(|e| e.profile)
        .unwrap_or_default()
}
pub fn mapped_effort(
    profile: &ModelProfile,
    requested: &str,
) -> std::result::Result<String, String> {
    let normalized = match requested {
        "medium" | "xhigh" if profile.thinking_format == Some(ThinkingFormat::Deepseek) => "high",
        "minimal" => "low",
        "ultra" => "max",
        other => other,
    };
    let Some(levels) = &profile.reasoning_efforts else {
        return Ok(normalized.into());
    };
    if levels.iter().any(|level| level == normalized) {
        return Ok(normalized.into());
    }
    let order = [
        "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra",
    ];
    let rank = order
        .iter()
        .position(|level| *level == normalized)
        .ok_or("不支持此思考档位")?;
    levels
        .iter()
        .filter_map(|level| {
            order
                .iter()
                .position(|known| known == level)
                .map(|position| (position.abs_diff(rank), level))
        })
        .min_by_key(|(distance, _)| *distance)
        .map(|(_, level)| level.clone())
        .ok_or_else(|| "当前接入路径明确不支持思考档位".into())
}
pub fn resolve(model: &ProviderModel) -> ModelCapabilities {
    let latest = known(&model.id);
    let matched = ModelCapabilities {
        image_input: latest
            .image_input
            .or(model.official_capabilities.image_input),
        parallel_tool_calls: latest
            .parallel_tool_calls
            .or(model.official_capabilities.parallel_tool_calls),
    };
    ModelCapabilities {
        image_input: model
            .capability_overrides
            .image_input
            .or(model.capabilities.image_input)
            .or(matched.image_input),
        parallel_tool_calls: model
            .capability_overrides
            .parallel_tool_calls
            .or(model.capabilities.parallel_tool_calls)
            .or(matched.parallel_tool_calls),
    }
}
pub fn merge_profile(primary: &ModelProfile, fallback: &ModelProfile) -> ModelProfile {
    ModelProfile {
        context_window: primary.context_window.or(fallback.context_window),
        max_input_tokens: primary.max_input_tokens.or(fallback.max_input_tokens),
        max_output_tokens: primary.max_output_tokens.or(fallback.max_output_tokens),
        reasoning_efforts: primary
            .reasoning_efforts
            .clone()
            .or_else(|| fallback.reasoning_efforts.clone()),
        default_effort: primary
            .default_effort
            .clone()
            .or_else(|| fallback.default_effort.clone()),
        thinking_format: primary.thinking_format.or(fallback.thinking_format),
        sampling_parameters: primary.sampling_parameters.or(fallback.sampling_parameters),
        tool_calls: primary.tool_calls.or(fallback.tool_calls),
        structured_output: primary.structured_output.or(fallback.structured_output),
        endpoints: EndpointSupport {
            messages: primary.endpoints.messages.or(fallback.endpoints.messages),
            chat_completions: primary
                .endpoints
                .chat_completions
                .or(fallback.endpoints.chat_completions),
            responses: primary.endpoints.responses.or(fallback.endpoints.responses),
        },
    }
}
pub fn profile(model: &ProviderModel) -> ModelProfile {
    let official = merge_profile(&known_profile(&model.id), &model.official_profile);
    let mut observed = model.profile.clone();
    if observed.reasoning_efforts.is_none() && !model.reasoning_efforts.is_empty() {
        observed.reasoning_efforts = Some(model.reasoning_efforts.clone());
    }
    let mut result = merge_profile(
        &model.profile_overrides,
        &merge_profile(&observed, &official),
    );
    if result.default_effort.as_ref().is_some_and(|default| {
        result
            .reasoning_efforts
            .as_ref()
            .is_some_and(|levels| !levels.contains(default))
    }) {
        result.default_effort = None;
    }
    result
}
pub fn enrich(model: &mut ProviderModel) {
    if let Some(entry) = crate::model_registry::entry(&model.id) {
        model.canonical_id = entry.ids.first().cloned();
        model.official_source = Some(entry.source);
        model.official_profile = entry.profile;
        model.official_capabilities = entry.capabilities;
    }
}
pub fn profile_from_upstream(item: &Value) -> ModelProfile {
    let number = |paths: &[&str]| {
        paths
            .iter()
            .find_map(|p| item.pointer(p).and_then(Value::as_i64))
            .filter(|n| (1..=100_000_000).contains(n))
    };
    let boolean = |paths: &[&str]| {
        paths
            .iter()
            .find_map(|p| item.pointer(p).and_then(Value::as_bool))
    };
    let mut levels = [
        "/effort/supported_levels",
        "/capabilities/effort/supported_levels",
        "/supported_reasoning_levels",
        "/reasoning_efforts",
    ]
    .iter()
    .find_map(|p| item.pointer(p).and_then(Value::as_array))
    .and_then(|a| {
        let levels: Option<Vec<String>> = a
            .iter()
            .map(|v| {
                v.as_str()
                    .or_else(|| v.get("effort").and_then(Value::as_str))
                    .filter(|s| crate::types::valid_effort(s))
                    .map(str::to_owned)
            })
            .collect();
        levels.filter(|values| {
            values.len() <= 8
                && values
                    .iter()
                    .collect::<std::collections::HashSet<_>>()
                    .len()
                    == values.len()
        })
    })
    .or_else(|| {
        let effort = item.pointer("/capabilities/effort")?.as_object()?;
        if effort.get("supported").and_then(Value::as_bool) == Some(false) {
            return Some(vec![]);
        }
        let levels: Vec<String> = ["low", "medium", "high", "xhigh", "max"]
            .into_iter()
            .filter(|level| {
                effort
                    .get(*level)
                    .and_then(|v| v.get("supported"))
                    .and_then(Value::as_bool)
                    == Some(true)
            })
            .map(str::to_owned)
            .collect();
        if levels.is_empty() {
            None
        } else {
            Some(levels)
        }
    });
    if boolean(&["/capabilities/effort/supported"]) == Some(false) {
        levels = Some(vec![]);
    }
    let endpoint_types = item
        .get("supported_endpoint_types")
        .and_then(Value::as_array)
        .filter(|a| !a.is_empty() && a.iter().all(Value::is_string));
    let endpoint =
        |name: &str| endpoint_types.map(|types| types.iter().any(|v| v.as_str() == Some(name)));
    let adaptive = boolean(&[
        "/capabilities/thinking/types/adaptive",
        "/capabilities/thinking/types/adaptive/supported",
        "/capabilities/thinking/adaptive/supported",
    ]);
    let budget = boolean(&[
        "/capabilities/thinking/types/enabled",
        "/capabilities/thinking/types/enabled/supported",
    ]);
    let thinking_format = if adaptive == Some(true) {
        Some(ThinkingFormat::Adaptive)
    } else if budget == Some(true) {
        Some(ThinkingFormat::Budget)
    } else if item.pointer("/effort/supported_levels").is_some()
        && item
            .pointer("/api_capabilities/anthropic_messages")
            .is_some()
    {
        Some(ThinkingFormat::Deepseek)
    } else if boolean(&["/capabilities/thinking/supported"]) == Some(false) {
        Some(ThinkingFormat::None)
    } else {
        None
    };
    ModelProfile {
        context_window: number(&[
            "/context_window",
            "/context_length",
            "/max_context_length",
            "/max_input_tokens",
        ]),
        max_input_tokens: number(&["/max_input_tokens"]),
        max_output_tokens: number(&["/max_output_tokens", "/max_tokens"]),
        default_effort: [
            "/effort/default_level",
            "/capabilities/effort/default_level",
        ]
        .iter()
        .find_map(|p| item.pointer(p).and_then(Value::as_str))
        .filter(|s| crate::types::valid_effort(s))
        .filter(|s| {
            levels
                .as_ref()
                .is_none_or(|values| values.iter().any(|value| value == *s))
        })
        .map(str::to_owned),
        reasoning_efforts: levels,
        thinking_format,
        sampling_parameters: boolean(&["/capabilities/sampling_parameters/supported"]),
        tool_calls: boolean(&[
            "/supports_tool_calls",
            "/capabilities/tool_calls/supported",
            "/capabilities/tool_use/supported",
        ]),
        structured_output: boolean(&["/capabilities/structured_outputs/supported"]),
        endpoints: EndpointSupport {
            messages: endpoint("anthropic").or_else(|| {
                item.pointer("/api_capabilities/anthropic_messages")
                    .filter(|v| v.is_object())
                    .map(|_| true)
            }),
            chat_completions: endpoint("openai"),
            responses: endpoint("openai-response"),
        },
    }
}

// Only explicit booleans or nonempty modality arrays are authoritative. Missing,
// null and malformed metadata do not mean "unsupported". Ignore upstream manual
// overrides: suppliers can report capabilities, but cannot change user choices.
pub fn from_upstream(item: &Value) -> ModelCapabilities {
    let boolean = |paths: &[&str]| {
        paths
            .iter()
            .find_map(|path| item.pointer(path).and_then(Value::as_bool))
    };
    let modalities = [
        "/input_modalities",
        "/modalities/input",
        "/architecture/input_modalities",
    ]
    .iter()
    .find_map(|path| item.pointer(path).and_then(Value::as_array))
    .filter(|values| !values.is_empty() && values.iter().all(Value::is_string));
    ModelCapabilities {
        image_input: boolean(&[
            "/supports_image_input",
            "/image_input",
            "/capabilities/imageInput",
            "/capabilities/image_input",
            "/capabilities/image_input/supported",
            "/capabilities/vision",
            "/capabilities/vision/supported",
        ])
        .or_else(|| modalities.map(|values| values.iter().any(|v| v.as_str() == Some("image")))),
        parallel_tool_calls: boolean(&[
            "/supports_parallel_tool_calls",
            "/parallel_tool_calls",
            "/capabilities/parallelToolCalls",
            "/capabilities/parallel_tool_calls",
            "/capabilities/parallel_tool_calls/supported",
        ]),
    }
}

/// Refresh only capabilities we own, preserving other catalog fields and choices.
pub fn repair_catalog(catalog: &mut Value, provider: &crate::types::StoredProvider) {
    if let Some(models) = catalog.get_mut("models").and_then(Value::as_array_mut) {
        for item in models {
            let Some(id) = item.get("slug").and_then(Value::as_str) else {
                continue;
            };
            let fallback = ProviderModel {
                id: id.into(),
                ..Default::default()
            };
            let model = provider
                .summary
                .codex_options
                .models
                .iter()
                .find(|m| m.id == id)
                .unwrap_or(&fallback);
            let caps = resolve(model);
            if let Some(object) = item.as_object_mut() {
                object.insert(
                    "input_modalities".into(),
                    if caps.image_input == Some(true) {
                        serde_json::json!(["text", "image"])
                    } else {
                        serde_json::json!(["text"])
                    },
                );
                object.insert(
                    "supports_parallel_tool_calls".into(),
                    caps.parallel_tool_calls.unwrap_or(false).into(),
                );
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    #[test]
    fn profiles_keep_unknown_false_and_manual_choices_distinct() {
        let mut model = ProviderModel {
            id: "claude-haiku-5-5".into(),
            profile: profile_from_upstream(
                &json!({"supported_endpoint_types":["anthropic","openai"],"capabilities":{"thinking":{"types":{"adaptive":{"supported":true}}},"effort":{"supported":false},"tool_use":{"supported":false}}}),
            ),
            ..Default::default()
        };
        let effective = profile(&model);
        assert_eq!(effective.thinking_format, Some(ThinkingFormat::Adaptive));
        assert_eq!(effective.reasoning_efforts, Some(vec![]));
        assert_eq!(effective.default_effort, None);
        assert_eq!(effective.tool_calls, Some(false));
        assert_eq!(effective.endpoints.responses, Some(false));
        model.profile_overrides.tool_calls = Some(true);
        model.profile_overrides.thinking_format = Some(ThinkingFormat::Budget);
        assert_eq!(profile(&model).tool_calls, Some(true));
        assert_eq!(
            profile(&model).thinking_format,
            Some(ThinkingFormat::Budget)
        );
        assert_eq!(profile_from_upstream(&json!({})), ModelProfile::default());
        let malformed = profile_from_upstream(
            &json!({"reasoning_efforts":["high","high"],"context_window":-1,"capabilities":{"tool_use":{"supported":"false"}}}),
        );
        assert_eq!(malformed, ModelProfile::default());
        let legacy = ProviderModel {
            id: "claude-haiku-5-5".into(),
            reasoning_efforts: vec!["low".into()],
            ..Default::default()
        };
        assert_eq!(profile(&legacy).reasoning_efforts, Some(vec!["low".into()]));
    }
    #[test]
    fn deepseek_metadata_and_effort_mapping_are_not_name_guesses() {
        let observed = profile_from_upstream(
            &json!({"context_window":1048576,"max_output_tokens":393216,"effort":{"supported_levels":["low","high","max"],"default_level":"high"},"api_capabilities":{"anthropic_messages":{"system_prompt_update":"leading-only"}}}),
        );
        assert_eq!(observed.thinking_format, Some(ThinkingFormat::Deepseek));
        assert_eq!(observed.endpoints.messages, Some(true));
        assert_eq!(observed.endpoints.responses, None);
        for (requested, mapped) in [
            ("minimal", "low"),
            ("medium", "high"),
            ("xhigh", "high"),
            ("ultra", "max"),
        ] {
            assert_eq!(mapped_effort(&observed, requested).unwrap(), mapped);
        }
        let invalid_default = profile_from_upstream(
            &json!({"effort":{"supported_levels":["high"],"default_level":"max"}}),
        );
        assert_eq!(invalid_default.default_effort, None);
        assert_eq!(
            known_profile("unknown/claude-haiku-5-5"),
            ModelProfile::default()
        );
        assert_eq!(
            known_profile("deepseek-v4-flash-vision-exp").thinking_format,
            None
        );
    }
    #[test]
    fn official_models_exceptions_and_unknown_ids() {
        for id in [
            "gpt-4o",
            "openai/gpt-4.1",
            "gpt-5.4-2026-03-05",
            "gpt-6.1-sol",
            "claude-sonnet-5-5",
            "anthropic/claude-opus-4-6",
            "gemini-3.8-flash",
            "deepseek-flash",
        ] {
            assert_eq!(known(id).image_input, Some(true), "{id}");
        }
        for id in ["gpt-3.5-turbo", "gpt-4", "o3-mini", "o1-mini"] {
            assert_eq!(known(id).image_input, Some(false), "{id}");
        }
        for id in [
            "gpt-99",
            "gpt-4o-custom",
            "custom/gpt-4o",
            "claude-new",
            "private-model",
        ] {
            assert_eq!(known(id).image_input, None, "{id}");
        }
    }
    #[test]
    fn upstream_false_and_manual_precedence() {
        let mut model = ProviderModel {
            id: "gpt-4o".into(),
            capabilities: from_upstream(
                &json!({"input_modalities":["text"],"supports_parallel_tool_calls":false}),
            ),
            ..Default::default()
        };
        assert_eq!(resolve(&model).image_input, Some(false));
        model.capability_overrides.image_input = Some(true);
        assert_eq!(resolve(&model).image_input, Some(true));
        model.capability_overrides.image_input = Some(false);
        model.capabilities.image_input = Some(true);
        assert_eq!(resolve(&model).image_input, Some(false));
        assert_eq!(
            from_upstream(&json!({"capabilities":{"vision":{"supported":true}}})).image_input,
            Some(true)
        );
        assert_eq!(
            from_upstream(&json!({"input_modalities":[],"supports_image_input":"false"}))
                .image_input,
            None
        );
        assert_eq!(
            from_upstream(&json!({"architecture":{"input_modalities":["text","image"]}}))
                .image_input,
            Some(true)
        );
    }
    #[test]
    fn old_model_records_remain_automatic() {
        let m: ProviderModel = serde_json::from_value(
            json!({"id":"gpt-4o","contextWindow":256000,"reasoningEfforts":[]}),
        )
        .unwrap();
        assert!(m.enabled);
        assert!(m.capability_overrides.image_input.is_none());
        assert_eq!(resolve(&m).image_input, Some(true));
    }
}
