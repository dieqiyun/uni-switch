//! Go-owned model naming and routing; no Rust protocol conversion lives here.
use crate::error::{AppError, Result};
use serde::Deserialize;
use serde_json::json;
#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ModelRoute {
    pub id: String,
    pub desktop_id: String,
    pub legacy_id: String,
    pub alternate_desktop_id: String,
}
#[derive(Deserialize)]
pub struct Catalog {
    pub routes: Vec<ModelRoute>,
    pub model: Option<String>,
}
pub fn catalog(
    models: &[crate::types::ProviderModel],
    default: &str,
    requested: Option<&str>,
) -> Result<Catalog> {
    let value = crate::metadata_transport::call(
        "gateway.catalog",
        json!({"models":super::core_models(models),"defaultModel":default,"requested":requested}),
    )?;
    serde_json::from_value(value).map_err(|_| AppError::new("model_route", "Go 模型路由响应无效"))
}
pub fn model_alias(model: &str) -> Result<String> {
    let models = [crate::types::ProviderModel {
        id: model.into(),
        enabled: true,
        ..Default::default()
    }];
    catalog(&models, model, None)?
        .routes
        .into_iter()
        .next()
        .map(|r| r.desktop_id)
        .ok_or_else(|| AppError::new("model_route", "模型路由不存在"))
}
pub fn legacy_model_alias(model: &str) -> Result<String> {
    let models = [crate::types::ProviderModel {
        id: model.into(),
        enabled: true,
        ..Default::default()
    }];
    catalog(&models, model, None)?
        .routes
        .into_iter()
        .next()
        .map(|r| r.legacy_id)
        .ok_or_else(|| AppError::new("model_route", "模型路由不存在"))
}
pub fn model_ids(provider: &crate::types::StoredProvider) -> Result<Vec<String>> {
    Ok(catalog(
        &provider.summary.codex_options.models,
        &provider.summary.model,
        None,
    )?
    .routes
    .into_iter()
    .map(|r| r.id)
    .collect())
}
#[cfg(test)]
pub fn resolve_model(provider: &crate::types::StoredProvider, requested: &str) -> Result<String> {
    catalog(
        &provider.summary.codex_options.models,
        &provider.summary.model,
        Some(requested),
    )?
    .model
    .ok_or_else(|| AppError::new("model_route", "模型路由不存在"))
}
