//! Projection of Go-resolved metadata. No local registry or permissive defaults
//! can overrule the catalog/vendor/user merge in the Go engine.
use crate::error::Result;
use crate::types::{ModelCapabilities, ModelProfile, ProviderModel, ResolvedMetadata};
use serde_json::{json, Value};

pub fn known_profile(id: &str) -> Result<ModelProfile> {
    profile(&ProviderModel {
        id: id.into(),
        ..Default::default()
    })
}
pub fn profile(model: &ProviderModel) -> Result<ModelProfile> {
    let result = crate::model_registry::resolve_one(model)?;
    let mut profile = result.profile;
    profile.effort_mapping = result.effort_mapping;
    Ok(profile)
}
pub fn resolve(model: &ProviderModel) -> Result<ModelCapabilities> {
    Ok(crate::model_registry::resolve_one(model)?.capabilities)
}
pub fn enrich(model: &mut ProviderModel) -> Result<()> {
    let base = model
        .metadata
        .as_ref()
        .map_or_else(String::new, |r| r.base_url.clone());
    crate::model_registry::resolve_models(std::slice::from_mut(model), &base, None)
}
pub fn parse_upstream(item: &Value) -> Result<ResolvedMetadata> {
    let value = crate::metadata_transport::call("metadata.parse", item.clone())?;
    serde_json::from_value(value)
        .map_err(|_| crate::error::AppError::new("go_metadata", "Go 模型条目结构不匹配"))
}

// Go calculates every supported menu/alias mapping using catalog.NearestEffort.
// Unknown request words remain unchanged, as in that function. Rust only reads
// the result and keeps the existing guard for explicitly unsupported reasoning.
pub fn mapped_effort(
    profile: &ModelProfile,
    requested: &str,
) -> std::result::Result<String, String> {
    let Some(levels) = &profile.reasoning_efforts else {
        return Ok(requested.into());
    };
    if levels.is_empty() {
        return Err("当前接入路径明确不支持思考档位".into());
    }
    Ok(profile
        .effort_mapping
        .get(requested)
        .cloned()
        .unwrap_or_else(|| requested.into()))
}

/// Refresh only metadata fields owned by the catalog writer. Native client
/// structural defaults remain at the serialization edge, not capability facts.
pub fn repair_catalog(catalog: &mut Value, provider: &crate::types::StoredProvider) -> Result<()> {
    *catalog = crate::adapters::call(
        "repairCatalog",
        json!({"catalog":catalog,"provider":provider}),
    )?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn no_implicit_image_or_parallel_support_and_user_false_wins() {
        let mut m = ProviderModel {
            id: "unknown-fixture-model".into(),
            ..Default::default()
        };
        assert_eq!(resolve(&m).unwrap(), ModelCapabilities::default());
        m.capability_overrides.image_input = Some(false);
        assert_eq!(resolve(&m).unwrap().image_input, Some(false));
        m.capability_overrides.image_input = Some(true);
        assert_eq!(resolve(&m).unwrap().image_input, Some(true));
    }
    #[test]
    fn vendor_fields_are_parsed_by_go_and_non_magpie_shapes_remain_unknown() {
        let r=parse_upstream(&json!({"id":"fixture","context_length":4095,"max_output_tokens":5000,"modalities":{"input":["text","image/png","audio"]},"supported_reasoning_levels":["light",{"effort":"extra_high"},"light"]})).unwrap();
        assert_eq!(r.profile.context_window, Some(4095));
        assert_eq!(r.published_output, Some(4095));
        assert_eq!(
            r.profile.reasoning_efforts,
            Some(vec!["light".into(), "extra_high".into()])
        );
        assert_eq!(r.capabilities.image_input, Some(true));
        let r=parse_upstream(&json!({"context_window":99999,"supports_image_input":true,"supports_tool_calls":true,"reasoning_efforts":["high"]})).unwrap();
        assert_eq!(r.profile, ModelProfile::default());
        assert_eq!(r.capabilities, ModelCapabilities::default());
    }
    #[test]
    fn fitting_keeps_aliases_and_ties_upward() {
        let mut m = ProviderModel {
            id: "effort-mapping-fixture".into(),
            ..Default::default()
        };
        m.profile.reasoning_efforts = Some(vec!["low".into(), "high".into()]);
        let p = profile(&m).unwrap();
        assert_eq!(mapped_effort(&p, "medium").unwrap(), "high");
        m.profile.reasoning_efforts = Some(vec!["light".into(), "extra_high".into()]);
        let p = profile(&m).unwrap();
        assert_eq!(mapped_effort(&p, "low").unwrap(), "light");
        assert_eq!(mapped_effort(&p, "xhigh").unwrap(), "extra_high");
        m.profile.reasoning_efforts = Some(vec!["none".into(), "high".into()]);
        let p = profile(&m).unwrap();
        assert_eq!(mapped_effort(&p, "low").unwrap(), "high");
    }
}
