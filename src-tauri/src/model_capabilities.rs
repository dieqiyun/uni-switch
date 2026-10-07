use crate::types::{ModelCapabilities, ProviderModel};
use serde::Deserialize;
use serde_json::Value;
use std::sync::OnceLock;

#[derive(Deserialize)]
struct Registry {
    entries: Vec<Entry>,
}
#[derive(Deserialize)]
struct Entry {
    ids: Vec<String>,
    capabilities: ModelCapabilities,
}

pub fn known(id: &str) -> ModelCapabilities {
    static REGISTRY: OnceLock<Registry> = OnceLock::new();
    let registry = REGISTRY.get_or_init(|| {
        serde_json::from_str(include_str!("../../src/content/model-capabilities.json"))
            .expect("verified model registry is valid")
    });
    let id = id.trim().to_ascii_lowercase();
    let id = ["openai/", "anthropic/", "google/", "deepseek/"]
        .iter()
        .find_map(|prefix| id.strip_prefix(prefix))
        .unwrap_or(&id);
    registry
        .entries
        .iter()
        .find(|entry| entry.ids.iter().any(|known| known == id))
        .map(|entry| entry.capabilities.clone())
        .unwrap_or_default()
}

pub fn resolve(model: &ProviderModel) -> ModelCapabilities {
    let matched = known(&model.id);
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
