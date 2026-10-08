use crate::error::{AppError, Result};
use crate::types::{ModelCapabilities, ModelProfile};
use base64::{engine::general_purpose::STANDARD, Engine};
use serde::{Deserialize, Serialize};
use std::{
    collections::HashSet,
    path::Path,
    sync::{OnceLock, RwLock},
};

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Registry {
    pub schema_version: u32,
    pub version: u64,
    pub verified_at: String,
    pub entries: Vec<Entry>,
}
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Entry {
    pub ids: Vec<String>,
    pub capabilities: ModelCapabilities,
    #[serde(default)]
    pub profile: ModelProfile,
    pub source: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub tool_source: Option<String>,
}
#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Status {
    pub registry: Registry,
    pub checked_at: Option<u64>,
    pub updated: bool,
    pub update_available: bool,
    pub message: String,
}
#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct UpdateConfig {
    model_registry: Option<UpdateSource>,
}
#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct UpdateSource {
    url: String,
    public_key: String,
}
#[derive(Serialize, Deserialize)]
struct SignedRegistry {
    payload: String,
    signature: String,
}

fn current() -> &'static RwLock<Registry> {
    static REGISTRY: OnceLock<RwLock<Registry>> = OnceLock::new();
    REGISTRY.get_or_init(|| {
        RwLock::new(
            serde_json::from_str(include_str!("../../src/content/model-capabilities.json"))
                .expect("bundled registry is valid"),
        )
    })
}
pub fn snapshot() -> Registry {
    current().read().unwrap_or_else(|e| e.into_inner()).clone()
}
pub fn validate_profile(profile: &ModelProfile) -> Result<()> {
    if [
        profile.context_window,
        profile.max_input_tokens,
        profile.max_output_tokens,
    ]
    .into_iter()
    .flatten()
    .any(|n| !(1..=100_000_000).contains(&n))
        || profile.reasoning_efforts.as_ref().is_some_and(|levels| {
            levels.len() > 8
                || levels.iter().any(|e| !crate::types::valid_effort(e))
                || levels.iter().collect::<HashSet<_>>().len() != levels.len()
        })
        || profile.default_effort.as_deref().is_some_and(|e| {
            !crate::types::valid_effort(e)
                || profile
                    .reasoning_efforts
                    .as_ref()
                    .is_some_and(|levels| !levels.iter().any(|v| v == e))
        })
    {
        return Err(AppError::new(
            "invalid_model_profile",
            "模型限制或思考档位无效",
        ));
    }
    Ok(())
}
pub fn entry(id: &str) -> Option<Entry> {
    let normalized = id.trim().to_ascii_lowercase();
    let normalized = ["openai/", "anthropic/", "google/", "deepseek/"]
        .iter()
        .find_map(|prefix| normalized.strip_prefix(prefix))
        .unwrap_or(&normalized);
    current()
        .read()
        .unwrap_or_else(|e| e.into_inner())
        .entries
        .iter()
        .find(|entry| entry.ids.iter().any(|id| id == normalized))
        .cloned()
}
fn validate(registry: &Registry) -> Result<()> {
    let mut ids = HashSet::new();
    let valid_date = registry.verified_at.len() == 10
        && registry
            .verified_at
            .bytes()
            .enumerate()
            .all(|(index, byte)| {
                if index == 4 || index == 7 {
                    byte == b'-'
                } else {
                    byte.is_ascii_digit()
                }
            });
    if registry.schema_version != 1
        || registry.version == 0
        || !valid_date
        || registry.entries.is_empty()
        || registry.entries.len() > 5000
    {
        return Err(AppError::new(
            "registry_invalid",
            "模型资料版本或格式不受支持",
        ));
    }
    for entry in &registry.entries {
        if entry.ids.is_empty()
            || entry.ids.len() > 100
            || entry.ids.iter().any(|id| {
                id.is_empty()
                    || id.len() > 256
                    || id != &id.trim().to_ascii_lowercase()
                    || id.chars().any(char::is_control)
                    || !ids.insert(id.clone())
            })
            || !url::Url::parse(&entry.source)
                .is_ok_and(|url| url.scheme() == "https" && url.host_str().is_some())
        {
            return Err(AppError::new(
                "registry_invalid",
                "模型资料包含无效型号或来源",
            ));
        }
        validate_profile(&entry.profile)?;
    }
    Ok(())
}
fn source() -> Result<UpdateSource> {
    let config: UpdateConfig = serde_json::from_str(include_str!("../../release-config.json"))
        .map_err(|_| AppError::new("registry_source", "模型资料更新配置无效"))?;
    let source = config.model_registry.ok_or_else(|| {
        AppError::new(
            "registry_not_configured",
            "独立模型资料更新尚未配置签名源；继续使用内置资料和供应商元数据",
        )
    })?;
    if crate::types::validate_url(&source.url)?.scheme() != "https"
        || STANDARD
            .decode(&source.public_key)
            .map_or(true, |key| key.len() != 32)
    {
        return Err(AppError::new("registry_source", "模型资料签名源无效"));
    }
    Ok(source)
}
fn verified(bytes: &[u8], signature: &str, public_key: &str) -> Result<Registry> {
    if bytes.len() > 2 * 1024 * 1024 || signature.len() > 256 {
        return Err(AppError::new("registry_size", "模型资料超过大小限制"));
    }
    let key = STANDARD
        .decode(public_key)
        .ok()
        .filter(|key| key.len() == 32)
        .ok_or_else(|| AppError::new("registry_signature", "模型资料公钥无效"))?;
    let signature = STANDARD
        .decode(signature.trim())
        .ok()
        .filter(|signature| signature.len() == 64)
        .ok_or_else(|| AppError::new("registry_signature", "模型资料签名无效"))?;
    ring::signature::UnparsedPublicKey::new(&ring::signature::ED25519, key)
        .verify(bytes, &signature)
        .map_err(|_| AppError::new("registry_signature", "模型资料签名不匹配，已保留原资料"))?;
    let registry: Registry = serde_json::from_slice(bytes)
        .map_err(|_| AppError::new("registry_invalid", "模型资料格式无效"))?;
    validate(&registry)?;
    Ok(registry)
}
fn check_version(previous: &Registry, candidate: &Registry) -> Result<()> {
    if candidate.version < previous.version
        || candidate.version == previous.version && candidate != previous
    {
        return Err(AppError::new(
            "registry_rollback",
            "模型资料版本较旧或同版本内容不同，原资料已保留",
        ));
    }
    Ok(())
}
pub fn initialize(directory: &Path) {
    let Ok(source) = source() else { return };
    if let Some(registry) = cached(directory, &source.public_key, &snapshot()) {
        *current().write().unwrap_or_else(|e| e.into_inner()) = registry;
    }
}
fn cached(directory: &Path, public_key: &str, previous: &Registry) -> Option<Registry> {
    let path = directory.join("model-registry-signed.json");
    if std::fs::metadata(&path).ok()?.len() > 3 * 1024 * 1024 {
        return None;
    }
    let bytes = std::fs::read(path).ok()?;
    let saved: SignedRegistry = serde_json::from_slice(&bytes).ok()?;
    let payload = STANDARD.decode(saved.payload).ok()?;
    let registry = verified(&payload, &saved.signature, public_key).ok()?;
    check_version(previous, &registry).ok()?;
    Some(registry)
}
pub fn status(directory: &Path) -> Status {
    let checked_at = std::fs::read_to_string(directory.join("model-registry-checked.json"))
        .ok()
        .and_then(|text| serde_json::from_str(&text).ok());
    Status {
        registry: snapshot(),
        checked_at,
        updated: false,
        update_available: source().is_ok(),
        message: "使用内置或最近一次验证通过的模型资料".into(),
    }
}
async fn download(client: &reqwest::Client, url: &str, limit: usize) -> Result<Vec<u8>> {
    let mut response =
        client.get(url).send().await.map_err(|_| {
            AppError::new("registry_network", "无法连接模型资料更新源，原资料已保留")
        })?;
    if !response.status().is_success() {
        return Err(AppError::new(
            "registry_http",
            format!(
                "模型资料下载失败（HTTP {}），原资料已保留",
                response.status().as_u16()
            ),
        ));
    }
    let mut bytes = Vec::new();
    while let Some(chunk) = response
        .chunk()
        .await
        .map_err(|_| AppError::new("registry_network", "模型资料下载中断"))?
    {
        if bytes.len() + chunk.len() > limit {
            return Err(AppError::new("registry_size", "模型资料超过大小限制"));
        }
        bytes.extend_from_slice(&chunk);
    }
    Ok(bytes)
}
pub async fn update(directory: &Path) -> Result<Status> {
    let source = source()?;
    let client = reqwest::Client::builder()
        .redirect(reqwest::redirect::Policy::none())
        .timeout(std::time::Duration::from_secs(20))
        .build()
        .map_err(|_| AppError::new("registry_network", "无法创建模型资料连接"))?;
    let bytes = download(&client, &source.url, 2 * 1024 * 1024).await?;
    let signature = download(&client, &format!("{}.sig", source.url), 256).await?;
    let signature = String::from_utf8(signature)
        .map_err(|_| AppError::new("registry_signature", "签名编码无效"))?;
    let registry = verified(&bytes, &signature, &source.public_key)?;
    let mut guard = current().write().unwrap_or_else(|e| e.into_inner());
    check_version(&guard, &registry)?;
    let updated = registry.version > guard.version;
    if updated {
        let signed = SignedRegistry {
            payload: STANDARD.encode(&bytes),
            signature,
        };
        let text = serde_json::to_string(&signed)
            .map_err(|_| AppError::new("registry_invalid", "模型资料无法保存"))?;
        crate::writer::write(&directory.join("model-registry-signed.json"), Some(&text))?;
        *guard = registry;
    }
    let registry = guard.clone();
    drop(guard);
    let checked_at = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs();
    crate::writer::write(
        &directory.join("model-registry-checked.json"),
        Some(&checked_at.to_string()),
    )?;
    Ok(Status {
        registry,
        checked_at: Some(checked_at),
        updated,
        update_available: true,
        message: if updated {
            "模型资料已更新，手动设置保留；重新同步并保存可更新客户端模型目录"
        } else {
            "模型资料已是最新"
        }
        .into(),
    })
}
pub async fn auto_update(directory: std::path::PathBuf) {
    if source().is_err() {
        return;
    }
    loop {
        let now = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap_or_default()
            .as_secs();
        if status(&directory)
            .checked_at
            .is_none_or(|checked| now.saturating_sub(checked) >= 86400)
        {
            let _ = update(&directory).await;
        }
        tokio::time::sleep(std::time::Duration::from_secs(86400)).await;
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use ring::signature::{Ed25519KeyPair, KeyPair};
    fn bundled() -> Registry {
        serde_json::from_str(include_str!("../../src/content/model-capabilities.json")).unwrap()
    }
    fn signing_key() -> Ed25519KeyPair {
        Ed25519KeyPair::from_seed_unchecked(&[7; 32]).unwrap()
    }
    #[test]
    fn bundled_schema_and_aliases_are_valid() {
        validate(&bundled()).unwrap();
        assert_eq!(
            entry("anthropic/claude-haiku-5-5")
                .unwrap()
                .profile
                .default_effort
                .as_deref(),
            Some("medium")
        );
        assert!(entry("claude-haiku-5-5-private").is_none());
    }
    #[test]
    fn signature_covers_exact_bytes_and_only_trusted_key() {
        let key = signing_key();
        let public = STANDARD.encode(key.public_key().as_ref());
        let bytes = serde_json::to_vec(&bundled()).unwrap();
        let signature = STANDARD.encode(key.sign(&bytes).as_ref());
        assert_eq!(verified(&bytes, &signature, &public).unwrap(), bundled());
        let mut tampered = bytes.clone();
        tampered.push(b' ');
        assert_eq!(
            verified(&tampered, &signature, &public).unwrap_err().code,
            "registry_signature"
        );
        let other = Ed25519KeyPair::from_seed_unchecked(&[8; 32]).unwrap();
        assert!(verified(
            &bytes,
            &signature,
            &STANDARD.encode(other.public_key().as_ref())
        )
        .is_err());
        assert!(verified(&bytes, "not-a-signature", &public).is_err());
        assert_eq!(
            verified(&vec![0; 2 * 1024 * 1024 + 1], &signature, &public)
                .unwrap_err()
                .code,
            "registry_size"
        );
    }
    #[test]
    fn invalid_schema_duplicates_limits_and_defaults_are_rejected() {
        let mut registry = bundled();
        registry.schema_version = 2;
        assert!(validate(&registry).is_err());
        let mut registry = bundled();
        registry.entries.push(registry.entries[0].clone());
        assert!(validate(&registry).is_err());
        let mut registry = bundled();
        registry.entries[0].profile.max_output_tokens = Some(0);
        assert!(validate(&registry).is_err());
        let mut registry = bundled();
        registry.entries[0].profile.reasoning_efforts = Some(vec!["high".into(), "high".into()]);
        assert!(validate(&registry).is_err());
        registry.entries[0].profile.reasoning_efforts = Some(vec!["high".into()]);
        registry.entries[0].profile.default_effort = Some("max".into());
        assert!(validate(&registry).is_err());
        let mut value = serde_json::to_value(bundled()).unwrap();
        value["unexpected"] = true.into();
        assert!(serde_json::from_value::<Registry>(value).is_err());
    }
    #[test]
    fn version_is_monotonic_and_equal_version_must_match() {
        let previous = bundled();
        let mut candidate = previous.clone();
        check_version(&previous, &candidate).unwrap();
        candidate.version -= 1;
        assert!(check_version(&previous, &candidate).is_err());
        candidate.version = previous.version;
        candidate.entries[0].capabilities.image_input = Some(false);
        assert!(check_version(&previous, &candidate).is_err());
        candidate.version += 1;
        check_version(&previous, &candidate).unwrap();
    }
    #[test]
    fn offline_cache_requires_valid_signature_schema_and_nonrollback() {
        let temp = tempfile::tempdir().unwrap();
        let previous = bundled();
        let mut next = previous.clone();
        next.version += 1;
        let key = signing_key();
        let public = STANDARD.encode(key.public_key().as_ref());
        assert!(cached(temp.path(), &public, &previous).is_none());
        let payload = serde_json::to_vec(&next).unwrap();
        let mut envelope = SignedRegistry {
            payload: STANDARD.encode(&payload),
            signature: STANDARD.encode(key.sign(&payload).as_ref()),
        };
        let path = temp.path().join("model-registry-signed.json");
        crate::writer::write(&path, Some(&serde_json::to_string(&envelope).unwrap())).unwrap();
        assert_eq!(cached(temp.path(), &public, &previous), Some(next.clone()));
        assert!(cached(
            temp.path(),
            &public,
            &Registry {
                version: next.version + 1,
                ..next
            }
        )
        .is_none());
        envelope.signature = "corrupted".into();
        crate::writer::write(&path, Some(&serde_json::to_string(&envelope).unwrap())).unwrap();
        assert!(cached(temp.path(), &public, &previous).is_none());
        assert_eq!(snapshot(), previous);
    }
}
