//! Data-only desktop adapter. Client rules, paths, formats and projections live in Go.
use crate::error::{AppError, Result};
use crate::types::{StoredProvider, Target};
use serde::{de::DeserializeOwned, Deserialize, Serialize};
use serde_json::{json, Value};
use std::path::{Path, PathBuf};
pub const PROFILE_UUID: &str = "e82de475-47fa-4c54-9000-13571c000001";
pub const REASONING_DISPLAY_KEY: &str = "desktop.enabled-reasoning-efforts";
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ManagedFile {
    pub path: PathBuf,
    pub format: String,
    pub keys: Vec<String>,
    pub original: Option<String>,
    pub expected: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Change {
    pub path: PathBuf,
    pub before: Option<String>,
    pub after: Option<String>,
}

pub(crate) fn call<T: DeserializeOwned>(op: &str, mut params: Value) -> Result<T> {
    params["cacheDirectory"] = json!(crate::model_registry::cache_directory());
    let v = crate::metadata_transport::call(&format!("agent.client.{op}"), params)?;
    serde_json::from_value(v).map_err(|_| AppError::new("go_config", "Go 客户端响应格式无效"))
}
pub fn read(path: &Path) -> Result<Option<String>> {
    call("read", json!({"path":path}))
}
pub fn default_directory(target: Target) -> Result<PathBuf> {
    call("defaultDirectory", json!({"client":target.id()}))
}
#[derive(Deserialize)]
struct Spec {
    path: PathBuf,
    format: String,
    keys: Vec<String>,
}
pub(crate) fn format_name(f: &str) -> Result<&'static str> {
    match f {
        "json" => Ok("json"),
        "toml" => Ok("toml"),
        "yaml" => Ok("yaml"),
 "jsonc" => Ok("jsonc"),
 "jsonc_array" => Ok("jsonc_array"),
 "env" => Ok("env"),
 "script" => Ok("script"),
        "newmax" => Ok("newmax"),
        "alma" => Ok("alma"),
        "zed_credential" => Ok("zed_credential"),
        "hanako_provider" => Ok("hanako_provider"),
        "hanako_chat" => Ok("hanako_chat"),
        "meta" => Ok("meta"),
        "catalog" => Ok("catalog"),
        _ => Err(AppError::new("go_config", "Go 配置格式无效")),
    }
}
pub fn specifications(
    target: Target,
    directory: &Path,
) -> Vec<(PathBuf, &'static str, Vec<String>)> {
    let specs: Vec<Spec> = call(
        "specifications",
        json!({"client":target.id(),"directory":directory}),
    )
    .expect("Go client path service unavailable");
    specs
        .into_iter()
        .map(|s| {
            (
                s.path,
                format_name(&s.format).expect("Go client format invalid"),
                s.keys,
            )
        })
        .collect()
}
pub fn plan(
    target: Target,
    directory: &Path,
    provider: &StoredProvider,
) -> Result<Vec<ManagedFile>> {
    call(
        "plan",
        json!({"client":target.id(),"directory":directory,"provider":provider}),
    )
}
pub fn model_catalog(provider: &StoredProvider) -> Result<Value> {
    call("catalog", json!({"provider":provider}))
}
pub fn fast_mode_files(
    baseline: &[ManagedFile],
    provider: &StoredProvider,
    enabled: bool,
) -> Result<Vec<ManagedFile>> {
    call(
        "fastFiles",
        json!({"files":baseline,"provider":provider,"enabled":enabled}),
    )
}
pub fn preserve_codex_choices(
    file: &ManagedFile,
    provider: &StoredProvider,
    keep_model: bool,
) -> Result<Option<String>> {
    call(
        "preserve",
        json!({"file":file,"provider":provider,"keep":keep_model}),
    )
}
pub fn repair_reasoning_catalog(catalog: &mut Value) -> Result<()> {
    *catalog = call("repairReasoning", json!({"catalog":catalog}))?;
    Ok(())
}
pub fn reasoning_repair_files(
    directory: &Path,
    provider: &StoredProvider,
) -> Result<Vec<ManagedFile>> {
    call(
        "repairFiles",
        json!({"directory":directory,"provider":provider}),
    )
}
pub fn claude_client_base_needs_update(files: &[ManagedFile]) -> Result<bool> {
    call("claudeBaseUpdate", json!({"files":files}))
}
pub fn projection(file: &ManagedFile, content: Option<&str>) -> Result<Value> {
    call("projection", json!({"file":file,"content":content}))
}
pub fn changed_for_provider(file: &ManagedFile, provider: Option<&StoredProvider>) -> Result<bool> {
    call("changed", json!({"file":file,"provider":provider}))
}
pub fn restore(file: &ManagedFile) -> Result<Change> {
    call("restore", json!({"file":file}))
}
#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
#[cfg(test)]
pub(crate) struct ClientModelPreferences {
    pub codex_options: crate::types::CodexOptions,
}
#[cfg(test)]
pub(crate) fn read_model_preferences(
    target: Target,
    directory: &Path,
) -> Result<ClientModelPreferences> {
    call(
        "preferences",
        json!({"client":target.id(),"directory":directory}),
    )
}
