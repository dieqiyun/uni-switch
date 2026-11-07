//! Desktop adapter for the Go models.dev cache and metadata resolver.
//! No signed/local Rust registry participates in metadata decisions.
use crate::error::{AppError, Result};
use crate::types::{ModelProfile, ProviderModel, ResolvedMetadata};
use serde_json::{json, Value};
use std::{
    collections::{HashMap, HashSet},
    path::{Path, PathBuf},
    sync::{Mutex, OnceLock, RwLock},
};

pub type Status = Value;

pub fn shutdown() {
    crate::metadata_transport::shutdown();
}
#[derive(Default, Clone)]
struct Config {
    directory: PathBuf,
    fallback: Option<PathBuf>,
}
fn config() -> &'static RwLock<Config> {
    static C: OnceLock<RwLock<Config>> = OnceLock::new();
    C.get_or_init(|| RwLock::new(Config::default()))
}
fn cache() -> &'static Mutex<HashMap<String, Vec<ResolvedMetadata>>> {
    static C: OnceLock<Mutex<HashMap<String, Vec<ResolvedMetadata>>>> = OnceLock::new();
    C.get_or_init(|| Mutex::new(HashMap::new()))
}
pub fn initialize(directory: &Path) {
    let fallback = if cfg!(test) || std::env::var_os("UNI_SWITCH_DATA_DIR").is_some() {
        None
    } else {
        dirs::home_dir().map(|p| p.join(".cache/opencode/models.json"))
    };
    *config().write().unwrap_or_else(|e| e.into_inner()) = Config {
        directory: directory.join("model-metadata"),
        fallback,
    };
    cache().lock().unwrap_or_else(|e| e.into_inner()).clear();
}
pub(crate) fn cache_directory() -> PathBuf {
    config()
        .read()
        .unwrap_or_else(|e| e.into_inner())
        .directory
        .clone()
}
fn settings(directory: &Path) -> Value {
    let c = config().read().unwrap_or_else(|e| e.into_inner());
    json!({"cacheDirectory":directory.join("model-metadata"),"fallbackCache":c.fallback.as_ref().map(|p|p.to_string_lossy()).unwrap_or_default()})
}
pub fn status(directory: &Path) -> Result<Status> {
    crate::metadata_transport::call("metadata.status", settings(directory))
}
pub async fn update(directory: &Path) -> Result<Status> {
    let mut p = settings(directory);
    p["force"] = json!(true);
    let result =
        tokio::task::spawn_blocking(move || crate::metadata_transport::call("metadata.update", p))
            .await
            .map_err(|_| AppError::new("go_metadata", "模型目录更新服务中断"))??;
    cache().lock().unwrap_or_else(|e| e.into_inner()).clear();
    Ok(result)
}
pub async fn auto_update(directory: PathBuf) {
    loop {
        let mut p = settings(&directory);
        p["force"] = json!(false);
        let _ = tokio::task::spawn_blocking(move || {
            crate::metadata_transport::call("metadata.update", p)
        })
        .await;
        tokio::time::sleep(std::time::Duration::from_secs(3600)).await;
    }
}
fn generation(c: &Config) -> Vec<(u64, u128)> {
    [
        Some(c.directory.join("models-dev.json")),
        Some(c.directory.join("models-labs.json")),
        c.fallback.clone(),
    ]
    .into_iter()
    .flatten()
    .map(|p| {
        std::fs::metadata(p)
            .map(|m| {
                (
                    m.len(),
                    m.modified()
                        .ok()
                        .and_then(|t| t.duration_since(std::time::UNIX_EPOCH).ok())
                        .map_or(0, |d| d.as_nanos()),
                )
            })
            .unwrap_or_default()
    })
    .collect()
}
pub fn resolve_models(
    models: &mut [ProviderModel],
    base: &str,
    provider_context: Option<i64>,
) -> Result<()> {
    if models.is_empty() {
        return Ok(());
    }
    let c = config().read().unwrap_or_else(|e| e.into_inner()).clone();
    let inputs:Vec<Value>=models.iter().map(|m|json!({
        "id":m.id,"contextWindow":m.context_window,"reasoningEfforts":m.reasoning_efforts,
        "profile":m.profile,"capabilities":m.capabilities,"profileOverrides":m.profile_overrides,"capabilityOverrides":m.capability_overrides,
        "metadata":m.metadata.as_ref().map(|r|json!({"vendor":r.vendor}))
    })).collect();
    let request = json!({"cacheDirectory":c.directory.to_string_lossy(),"fallbackCache":c.fallback.as_ref().map(|p|p.to_string_lossy()).unwrap_or_default(),"baseUrl":base,"providerContext":provider_context.unwrap_or(0),"models":inputs});
    let key = serde_json::to_string(&(generation(&c), &request))
        .map_err(|_| AppError::new("go_metadata", "模型资料无法编码"))?;
    let cached = cache()
        .lock()
        .unwrap_or_else(|e| e.into_inner())
        .get(&key)
        .cloned();
    let resolved: Vec<ResolvedMetadata> = if let Some(found) = cached {
        found
    } else {
        let result = crate::metadata_transport::call("metadata.resolve", request)?;
        let result: Vec<ResolvedMetadata> = serde_json::from_value(result)
            .map_err(|_| AppError::new("go_metadata", "Go 模型资料结构不匹配"))?;
        if result.len() != models.len() || result.iter().any(|r| r.policy != "magpie-model-spec-v1")
        {
            return Err(AppError::new("go_metadata", "Go 模型资料规则版本不匹配"));
        }
        for r in &result {
            validate_profile(&r.profile)?;
        }
        let mut cache = cache().lock().unwrap_or_else(|e| e.into_inner());
        if cache.len() >= 1024 {
            cache.clear();
        };
        cache.insert(key, result.clone());
        result
    };
    for (m, r) in models.iter_mut().zip(resolved) {
        m.official_source = r.catalog.as_ref().map(|f| {
            f["provider"]
                .as_str()
                .filter(|v| !v.is_empty())
                .map(|p| format!("https://models.dev/api.json#{p}"))
                .unwrap_or_else(|| "https://models.dev/api.json".into())
        });
        m.official_profile = ModelProfile::default();
        m.official_capabilities = Default::default();
        m.canonical_id = None;
        m.reasoning_efforts = r.profile.reasoning_efforts.clone().unwrap_or_default();
        m.metadata = Some(r);
    }
    Ok(())
}
pub fn resolve_one(model: &ProviderModel) -> Result<ResolvedMetadata> {
    let mut models = [model.clone()];
    let base = model.metadata.as_ref().map_or("", |r| r.base_url.as_str());
    let context = model.metadata.as_ref().and_then(|r| r.provider_context);
    resolve_models(&mut models, base, context)?;
    models[0]
        .metadata
        .take()
        .ok_or_else(|| AppError::new("go_metadata", "Go 后端未返回模型资料"))
}
pub fn valid_vendor_effort(e: &str) -> bool {
    !e.is_empty() && e.len() <= 64 && !e.chars().any(char::is_control)
}
pub fn validate_profile(p: &ModelProfile) -> Result<()> {
    if [
        p.context_window,
        p.max_context_window,
        p.max_input_tokens,
        p.max_output_tokens,
    ]
    .into_iter()
    .flatten()
    .any(|n| !(1..=100_000_000).contains(&n))
        || p.reasoning_efforts.as_ref().is_some_and(|v| {
            v.len() > 64
                || v.iter().any(|e| !valid_vendor_effort(e))
                || v.iter().collect::<HashSet<_>>().len() != v.len()
        })
        || p.default_effort
            .as_deref()
            .is_some_and(|e| !valid_vendor_effort(e))
        || [&p.input_modalities, &p.output_modalities]
            .into_iter()
            .flatten()
            .any(|v| v.len() > 32 || v.iter().any(|s| !valid_vendor_effort(s)))
    {
        return Err(AppError::new(
            "invalid_model_profile",
            "模型限制或供应商思考档位无效",
        ));
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn vendor_words_and_absent_defaults_are_not_fixed_to_our_menu() {
        let p = ModelProfile {
            reasoning_efforts: Some(vec!["light".into(), "extra_high".into()]),
            default_effort: Some("auto".into()),
            ..Default::default()
        };
        validate_profile(&p).unwrap();
        assert!(!crate::types::valid_effort("extra_high"));
        assert!(!valid_vendor_effort("high\nsecret"));
    }
    #[test]
    fn go_merge_ignores_frontend_official_guesses_and_preserves_user_false() {
        let mut m = ProviderModel {
            id: "unlisted-fixture".into(),
            ..Default::default()
        };
        m.official_profile.context_window = Some(999_999);
        m.official_capabilities.image_input = Some(true);
        let r = resolve_one(&m).unwrap();
        assert_eq!(r.profile.context_window, None);
        assert_eq!(r.capabilities.image_input, None);
        m.profile_overrides.max_output_tokens = Some(8192);
        m.capability_overrides.image_input = Some(false);
        let r = resolve_one(&m).unwrap();
        assert_eq!(r.capabilities.image_input, Some(false));
        assert_eq!(r.profile.max_output_tokens, Some(8192));
    }
}
