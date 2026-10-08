use crate::error::{AppError, Result};
use crate::types::{StoredProvider, Target};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::path::{Path, PathBuf};
use toml_edit::{DocumentMut, Item, Table};

// A UUID is required by the desktop configuration library.
pub const PROFILE_UUID: &str = "e82de475-47fa-4c54-9000-13571c000001";
pub const REASONING_DISPLAY_KEY: &str = "desktop.enabled-reasoning-efforts";
const CODEX_KEYS: &[&str] = &[
    "model",
    "model_provider",
    "model_reasoning_effort",
    "service_tier",
    "features.fast_mode",
    "model_context_window",
    "model_auto_compact_token_limit",
    "model_catalog_json",
    "model_providers.uni_switch",
    "web_search",
    REASONING_DISPLAY_KEY,
];
const CLI_KEYS: &[&str] = &[
    "model",
    "apiKeyHelper",
    "env.ANTHROPIC_BASE_URL",
    "env.ANTHROPIC_AUTH_TOKEN",
    "env.ANTHROPIC_API_KEY",
    "env.ANTHROPIC_MODEL",
    "env.ANTHROPIC_DEFAULT_SONNET_MODEL",
    "env.ANTHROPIC_DEFAULT_OPUS_MODEL",
    "env.ANTHROPIC_DEFAULT_HAIKU_MODEL",
    "env.CLAUDE_CODE_USE_BEDROCK",
    "env.CLAUDE_CODE_USE_VERTEX",
    "env.CLAUDE_CODE_USE_FOUNDRY",
];
const PROFILE_KEYS: &[&str] = &[
    "inferenceProvider",
    "inferenceGatewayBaseUrl",
    "inferenceGatewayApiKey",
    "inferenceGatewayAuthScheme",
    "inferenceCredentialKind",
    "inferenceCredentialHelper",
    "inferenceCredentialHelperWindows",
    "inferenceGatewayOidc",
    "inferenceIdpOidc",
    "inferenceModels",
    "modelDiscoveryEnabled",
    "disableDeploymentModeChooser",
];

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

pub fn read(path: &Path) -> Result<Option<String>> {
    match std::fs::read(path) {
        Ok(bytes) => String::from_utf8(bytes)
            .map(|text| Some(text.trim_start_matches('\u{feff}').to_owned()))
            .map_err(|_| {
                AppError::new(
                    "invalid_encoding",
                    format!("{} 不是有效的 UTF-8 文件", path.display()),
                )
            }),
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(err) => Err(AppError::io(path, err)),
    }
}

fn parse_json(text: Option<&str>, path: &Path) -> Result<Value> {
    let doc = serde_json::from_str::<Value>(text.unwrap_or("{}")).map_err(|err| {
        AppError::new(
            "invalid_json",
            format!(
                "{} 的 JSON 无效（第 {} 行，第 {} 列），请修复后再应用",
                path.display(),
                err.line(),
                err.column()
            ),
        )
    })?;
    if !doc.is_object() {
        return Err(AppError::new(
            "invalid_json",
            format!("{} 的顶层必须是 JSON 对象", path.display()),
        ));
    }
    Ok(doc)
}

fn parse_toml(text: Option<&str>, path: &Path) -> Result<DocumentMut> {
    text.unwrap_or("").parse::<DocumentMut>().map_err(|_| {
        AppError::new(
            "invalid_toml",
            format!("{} 的 TOML 语法无效，请修复后再应用", path.display()),
        )
    })
}

fn json_text(value: &Value) -> String {
    format!(
        "{}\n",
        serde_json::to_string_pretty(value).expect("JSON Value serializes")
    )
}

pub fn default_directory(target: Target) -> Result<PathBuf> {
    let home =
        dirs::home_dir().ok_or_else(|| AppError::new("missing_home", "无法确定当前用户目录"))?;
    Ok(match target {
        Target::Codex => std::env::var_os("CODEX_HOME")
            .map(PathBuf::from)
            .filter(|p| p.is_absolute())
            .unwrap_or_else(|| home.join(".codex")),
        Target::ClaudeCli => std::env::var_os("CLAUDE_CONFIG_DIR")
            .map(PathBuf::from)
            .filter(|p| p.is_absolute())
            .unwrap_or_else(|| home.join(".claude")),
        Target::ClaudeDesktop => {
            #[cfg(windows)]
            {
                std::env::var_os("LOCALAPPDATA")
                    .map(PathBuf::from)
                    .unwrap_or_else(|| home.join("AppData/Local"))
            }
            #[cfg(target_os = "macos")]
            {
                home.join("Library/Application Support")
            }
            #[cfg(all(not(windows), not(target_os = "macos")))]
            {
                dirs::config_dir().unwrap_or_else(|| home.join(".config"))
            }
        }
    })
}

fn desktop_dir(root: &Path, third_party: bool) -> PathBuf {
    // An explicitly selected app directory represents one deployment pair.
    // This avoids choosing the first Claude sibling when several installs exist.
    let app_name = root.file_name().and_then(|n| n.to_str()).unwrap_or("");
    if root.join("claude_desktop_config.json").is_file()
        || root.join("configLibrary/_meta.json").is_file()
    {
        let is_third = app_name.contains("-3p") || root.join("configLibrary/_meta.json").is_file();
        // A custom Electron user-data root can hold both deployment settings
        // and the profile library without a separate '-3p' sibling.
        if is_third && !app_name.contains("-3p") {
            return root.to_path_buf();
        }
        if is_third == third_party {
            return root.to_path_buf();
        }
        if let Some(parent) = root.parent() {
            let sibling = if third_party {
                format!("{app_name}-3p")
            } else {
                app_name.replace("-3p", "")
            };
            if !sibling.is_empty() {
                return parent.join(sibling);
            }
        }
    }
    let name = if third_party { "Claude-3p" } else { "Claude" };
    let exact = root.join(name);
    if exact.exists() {
        return exact;
    }
    #[cfg(windows)]
    {
        let mut candidates: Vec<_> = std::fs::read_dir(root)
            .ok()
            .into_iter()
            .flatten()
            .filter_map(|entry| entry.ok())
            .map(|entry| entry.path())
            .filter(|path| {
                path.is_dir()
                    && path.file_name().and_then(|n| n.to_str()).is_some_and(|n| {
                        n.starts_with("Claude")
                            && n.contains("-3p") == third_party
                            && (path.join("claude_desktop_config.json").is_file()
                                || path.join("configLibrary/_meta.json").is_file())
                    })
            })
            .collect();
        candidates.sort();
        if candidates.len() == 1 {
            return candidates.remove(0);
        }
    }
    exact
}

pub fn specifications(
    target: Target,
    directory: &Path,
) -> Vec<(PathBuf, &'static str, Vec<String>)> {
    let owned = |keys: &[&str]| keys.iter().map(|key| (*key).to_owned()).collect();
    match target {
        Target::Codex => vec![
            (directory.join("config.toml"), "toml", owned(CODEX_KEYS)),
            (directory.join("uni-switch-models.json"), "catalog", vec![]),
        ],
        Target::ClaudeCli => vec![(directory.join("settings.json"), "json", owned(CLI_KEYS))],
        Target::ClaudeDesktop => {
            let normal = desktop_dir(directory, false);
            let third = desktop_dir(directory, true);
            let mut specs = vec![
                (
                    normal.join("claude_desktop_config.json"),
                    "json",
                    owned(&["deploymentMode"]),
                ),
                (
                    third.join("claude_desktop_config.json"),
                    "json",
                    owned(&["deploymentMode"]),
                ),
                (
                    third.join(format!("configLibrary/{PROFILE_UUID}.json")),
                    "json",
                    owned(PROFILE_KEYS),
                ),
                (
                    third.join("configLibrary/_meta.json"),
                    "meta",
                    owned(&["appliedId", "entries"]),
                ),
            ];
            let mut seen = std::collections::HashSet::new();
            specs.retain(|spec| seen.insert(spec.0.clone()));
            specs
        }
    }
}

pub fn plan(
    target: Target,
    directory: &Path,
    provider: &StoredProvider,
) -> Result<Vec<ManagedFile>> {
    if target.family() != provider.summary.family {
        return Err(AppError::new("wrong_family", "该配置不属于此客户端"));
    }
    if target == Target::Codex
        && !provider.summary.codex_options.models.is_empty()
        && !provider
            .summary
            .codex_options
            .models
            .iter()
            .any(|m| m.enabled)
    {
        return Err(AppError::new("no_enabled_models", "请至少启用一个模型"));
    }
    if target == Target::ClaudeDesktop && !desktop_model_valid(&provider.summary.model) {
        return Err(AppError::new("desktop_model", "Claude 桌面端需要可识别的完整 Claude 模型 ID，例如 claude-sonnet-4-6；其他协议的模型需要代理转换，当前版本不支持"));
    }
    let mut enabled_models: Vec<&str> = provider
        .summary
        .codex_options
        .models
        .iter()
        .filter(|m| m.enabled)
        .map(|m| m.id.as_str())
        .collect();
    enabled_models.retain(|id| *id != provider.summary.model);
    enabled_models.insert(0, &provider.summary.model);
    if target == Target::ClaudeDesktop && enabled_models.iter().any(|id| !desktop_model_valid(id)) {
        return Err(AppError::new(
            "desktop_model",
            "启用列表中包含 Claude 桌面端不支持的模型，请取消勾选后再应用",
        ));
    }
    let mut files = Vec::new();
    for (path, format, mut keys) in specifications(target, directory) {
        if target == Target::Codex
            && provider.summary.codex_options.protocol != crate::types::CodexProtocol::Anthropic
        {
            keys.retain(|key| key != "web_search");
        }
        let before = read(&path)?;
        let after = if format == "toml" {
            let mut doc = parse_toml(before.as_deref(), &path)?;
            if let Some(profile) = doc.get("profile").and_then(Item::as_str) {
                if doc
                    .get("profiles")
                    .and_then(|v| v.get(profile))
                    .is_some_and(|p| CODEX_KEYS.iter().any(|key| p.get(key).is_some()))
                {
                    return Err(AppError::new(
                        "profile_override",
                        "当前 Codex profile 覆盖了模型、供应商或性能设置，请先移除该覆盖再切换",
                    ));
                }
            }
            let mut table = Table::new();
            table.insert("name", toml_edit::value(&provider.summary.name));
            table.insert("base_url", toml_edit::value(&provider.summary.base_url));
            table.insert("wire_api", toml_edit::value("responses"));
            if provider.summary.auth_mode == "x-api-key" {
                let mut headers = toml_edit::InlineTable::new();
                headers.insert(
                    "x-api-key",
                    toml_edit::Value::from(provider.api_key.clone()),
                );
                table.insert(
                    "http_headers",
                    Item::Value(toml_edit::Value::InlineTable(headers)),
                );
            } else {
                table.insert(
                    "experimental_bearer_token",
                    toml_edit::value(&provider.api_key),
                );
            }
            let auth_path = directory.join("auth.json");
            let auth = read(&auth_path)?;
            let logged_in = auth
                .as_deref()
                .and_then(|text| serde_json::from_str::<Value>(text).ok())
                .is_some_and(|v| {
                    v.get("tokens").is_some_and(|t| {
                        t.get("access_token")
                            .and_then(Value::as_str)
                            .is_some_and(|s| !s.is_empty())
                    })
                });
            let store = doc
                .get("cli_auth_credentials_store")
                .and_then(Item::as_str)
                .unwrap_or("file");
            if matches!(store, "keyring" | "auto") {
                return Err(AppError::new("credential_store", "当前 Codex 使用系统凭据库。此版本尚未验证该认证模式，请使用 cli_auth_credentials_store = \"file\" 后再应用；原登录未修改"));
            }
            table.insert(
                "requires_openai_auth",
                toml_edit::value(logged_in && provider.summary.auth_mode != "x-api-key"),
            );
            doc["model"] = toml_edit::value(&provider.summary.model);
            doc["model_provider"] = toml_edit::value("uni_switch");
            if let Some(effort) = &provider.summary.reasoning_effort {
                doc["model_reasoning_effort"] = toml_edit::value(effort);
            } else if doc
                .get("model_reasoning_effort")
                .is_some_and(|value| !value.as_str().is_some_and(crate::types::valid_effort))
            {
                doc.as_table_mut().remove("model_reasoning_effort");
            }
            // No supplier override means the user chooses effort in Codex.
            // Reapplying or editing the supplier must not reset a valid choice
            // such as xhigh to the model's implicit default.
            let options = &provider.summary.codex_options;
            if options.protocol == crate::types::CodexProtocol::Anthropic {
                doc["web_search"] = toml_edit::value("disabled");
            }
            repair_desktop_reasoning_display(&mut doc)?;
            if let Some(fast) = options.fast_mode {
                doc["service_tier"] = toml_edit::value(if fast { "priority" } else { "default" });
                if doc.get("features").is_none() {
                    doc["features"] = Item::Table(Table::new());
                }
                let features = doc
                    .get_mut("features")
                    .and_then(Item::as_table_like_mut)
                    .ok_or_else(|| {
                        AppError::new("invalid_shape", "Codex 的 features 必须是 TOML 表")
                    })?;
                features.insert("fast_mode", toml_edit::value(true));
            } else {
                doc.as_table_mut().remove("service_tier");
                if let Some(features) = doc.get_mut("features").and_then(Item::as_table_like_mut) {
                    features.remove("fast_mode");
                }
            }
            for (key, value) in [
                ("model_context_window", options.context_window),
                (
                    "model_auto_compact_token_limit",
                    options.auto_compact_token_limit,
                ),
            ] {
                if let Some(value) = value {
                    doc[key] = toml_edit::value(value);
                } else {
                    doc.as_table_mut().remove(key);
                }
            }
            doc["model_catalog_json"] = toml_edit::value(
                directory
                    .join("uni-switch-models.json")
                    .to_string_lossy()
                    .as_ref(),
            );
            if doc.get("model_providers").is_none() {
                doc["model_providers"] = Item::Table(Table::new());
            }
            let providers = doc
                .get_mut("model_providers")
                .and_then(Item::as_table_like_mut)
                .ok_or_else(|| {
                    AppError::new("invalid_shape", "Codex 的 model_providers 必须是 TOML 表")
                })?;
            providers.insert("uni_switch", Item::Table(table));
            doc.to_string()
        } else if format == "catalog" {
            json_text(&model_catalog(provider))
        } else {
            let mut doc = parse_json(before.as_deref(), &path)?;
            match target {
                Target::ClaudeCli => {
                    if provider.summary.codex_options.claude_protocol
                        == crate::types::ClaudeProtocol::Openai
                    {
                        keys.push("env.ENABLE_TOOL_SEARCH".into());
                    }
                    for key in &keys {
                        remove_json(&mut doc, key)?;
                    }
                    set_json(
                        &mut doc,
                        "env.ANTHROPIC_BASE_URL",
                        Some(json!(claude_client_base_url(&provider.summary.base_url))),
                    )?;
                    let auth = if provider.summary.auth_mode == "x-api-key" {
                        "env.ANTHROPIC_API_KEY"
                    } else {
                        "env.ANTHROPIC_AUTH_TOKEN"
                    };
                    set_json(&mut doc, auth, Some(json!(provider.api_key)))?;
                    set_json(
                        &mut doc,
                        "env.ANTHROPIC_MODEL",
                        Some(json!(provider.summary.model)),
                    )?;
                    set_json(&mut doc, "model", Some(json!(provider.summary.model)))?;
                    if provider.summary.codex_options.claude_protocol
                        == crate::types::ClaudeProtocol::Openai
                    {
                        set_json(&mut doc, "env.ENABLE_TOOL_SEARCH", Some(json!("false")))?;
                    }
                    for tier in ["SONNET", "OPUS", "HAIKU"] {
                        let tier_model = enabled_models
                            .iter()
                            .find(|id| id.contains(&format!("claude-{}-", tier.to_lowercase())))
                            .copied()
                            .unwrap_or(&provider.summary.model);
                        set_json(
                            &mut doc,
                            &format!("env.ANTHROPIC_DEFAULT_{tier}_MODEL"),
                            Some(json!(tier_model)),
                        )?;
                    }
                }
                Target::ClaudeDesktop if format == "meta" => {
                    let obj = doc.as_object_mut().unwrap();
                    let entries = obj
                        .entry("entries")
                        .or_insert(json!([]))
                        .as_array_mut()
                        .ok_or_else(|| {
                            AppError::new("invalid_shape", "Claude 配置库 entries 必须是数组")
                        })?;
                    if !entries
                        .iter()
                        .any(|entry| entry.get("id").and_then(Value::as_str) == Some(PROFILE_UUID))
                    {
                        entries.push(json!({"id": PROFILE_UUID, "name": "uni-switch"}));
                    }
                    obj.insert("appliedId".into(), json!(PROFILE_UUID));
                }
                Target::ClaudeDesktop
                    if path.file_name().and_then(|s| s.to_str())
                        == Some("claude_desktop_config.json") =>
                {
                    doc["deploymentMode"] = json!("3p");
                }
                Target::ClaudeDesktop => {
                    for key in &keys {
                        remove_json(&mut doc, key)?;
                    }
                    doc["inferenceProvider"] = json!("gateway");
                    doc["inferenceGatewayBaseUrl"] =
                        json!(claude_client_base_url(&provider.summary.base_url));
                    doc["inferenceGatewayApiKey"] = json!(provider.api_key);
                    doc["inferenceGatewayAuthScheme"] = json!(provider.summary.auth_mode);
                    doc["inferenceCredentialKind"] = json!("static");
                    doc["inferenceModels"] = json!(enabled_models);
                    doc["modelDiscoveryEnabled"] = json!(false);
                    doc["disableDeploymentModeChooser"] = json!(true);
                }
                _ => unreachable!(),
            }
            json_text(&doc)
        };
        files.push(ManagedFile {
            path,
            format: format.into(),
            keys,
            original: before,
            expected: Some(after),
        });
    }
    Ok(files)
}

pub fn model_catalog(provider: &StoredProvider) -> Value {
    // Older providers have only a default model. Give them the same automatic
    // display repair without changing their saved provider metadata.
    let fallback = [crate::types::ProviderModel {
        id: provider.summary.model.clone(),
        context_window: None,
        reasoning_efforts: vec![],
        enabled: true,
        ..Default::default()
    }];
    let source = if provider.summary.codex_options.models.is_empty() {
        fallback.as_slice()
    } else {
        provider.summary.codex_options.models.as_slice()
    };
    let models: Vec<Value> = source.iter().filter(|m| m.enabled).enumerate().map(|(index, model)| {
        let efforts: Vec<_> = model.reasoning_efforts.iter().map(|effort| json!({"effort": effort, "description": effort})).collect();
        let capabilities = crate::model_capabilities::resolve(model);
        json!({
            "slug": model.id, "display_name": model.id,
            "description": "供应商模型 · uni-switch", "visibility": "list", "supported_in_api": true,
            "priority": if model.id == provider.summary.model { 0 } else { index + 1 },
            "default_reasoning_level": model.reasoning_efforts.first(), "supported_reasoning_levels": efforts,
            "shell_type": "shell_command", "availability_nux": null, "upgrade": null,
            "base_instructions": include_str!("../resources/codex-base-instructions.md"), "model_messages": null,
            "support_verbosity": false, "default_verbosity": null, "apply_patch_tool_type": if provider.summary.codex_options.protocol == crate::types::CodexProtocol::Anthropic {json!("freeform")} else {Value::Null},
            "supports_reasoning_summary_parameter": false, "default_reasoning_summary": "none",
            "context_window": model.context_window.or(provider.summary.codex_options.context_window).unwrap_or(256_000),
            "max_context_window": model.context_window.or(provider.summary.codex_options.context_window).unwrap_or(256_000),
            "auto_compact_token_limit": model.context_window.or(provider.summary.codex_options.context_window).unwrap_or(256_000),
            "truncation_policy": {"mode": "tokens", "limit": 10000},
            "experimental_supported_tools": [], "input_modalities": if capabilities.image_input == Some(true) {vec!["text", "image"]} else {vec!["text"]},
            "supports_parallel_tool_calls": capabilities.parallel_tool_calls.unwrap_or(false),
            "additional_speed_tiers": if provider.summary.codex_options.fast_mode == Some(true) { vec!["fast"] } else { vec![] },
            "service_tiers": if provider.summary.codex_options.fast_mode == Some(true) { vec![json!({"id":"priority", "name":"Fast", "description":"供应商优先服务档位"})] } else { vec![] }
        })
    }).collect();
    let mut catalog = json!({"models": models});
    if !catalog["models"].as_array().unwrap().is_empty() {
        repair_reasoning_catalog(&mut catalog).expect("generated model catalog is valid");
    }
    catalog
}

/// Change only the Fast settings in the currently applied files. Preserve the
/// client's model/effort choices and all existing model catalog metadata.
pub fn fast_mode_files(
    baseline: &[ManagedFile],
    provider: &StoredProvider,
    enabled: bool,
) -> Result<Vec<ManagedFile>> {
    let mut files = Vec::new();
    for file in baseline
        .iter()
        .filter(|f| matches!(f.format.as_str(), "toml" | "catalog"))
    {
        let before = read(&file.path)?;
        let after = if file.format == "toml" {
            let mut doc = parse_toml(before.as_deref(), &file.path)?;
            doc["service_tier"] = toml_edit::value(if enabled { "priority" } else { "default" });
            if doc.get("features").is_none() {
                doc["features"] = Item::Table(Table::new());
            }
            let features = doc
                .get_mut("features")
                .and_then(Item::as_table_like_mut)
                .ok_or_else(|| {
                    AppError::new("invalid_shape", "Codex 的 features 必须是 TOML 表")
                })?;
            features.insert("fast_mode", toml_edit::value(true));
            repair_desktop_reasoning_display(&mut doc)?;
            doc["model_catalog_json"] = toml_edit::value(
                file.path
                    .parent()
                    .unwrap()
                    .join("uni-switch-models.json")
                    .to_string_lossy()
                    .as_ref(),
            );
            doc.to_string()
        } else {
            let mut catalog = match &before {
                Some(text) => parse_json(Some(text), &file.path)?,
                None => model_catalog(provider),
            };
            if let Some(expected) = &file.expected {
                fill_missing_reasoning_presets(
                    &mut catalog,
                    &parse_json(Some(expected), &file.path)?,
                );
            }
            crate::model_capabilities::repair_catalog(&mut catalog, provider);
            repair_reasoning_catalog(&mut catalog)?;
            let models = catalog
                .get_mut("models")
                .and_then(Value::as_array_mut)
                .ok_or_else(|| {
                    AppError::new("invalid_catalog", "模型目录格式无效，请重新同步模型后重试")
                })?;
            for model in models {
                let model = model.as_object_mut().ok_or_else(|| {
                    AppError::new("invalid_catalog", "模型目录格式无效，请重新同步模型后重试")
                })?;
                model.insert(
                    "additional_speed_tiers".into(),
                    if enabled { json!(["fast"]) } else { json!([]) },
                );
                model.insert("service_tiers".into(), if enabled {
                    json!([{"id":"priority", "name":"Fast", "description":"供应商优先服务档位"}])
                } else { json!([]) });
            }
            json_text(&catalog)
        };
        let mut keys = file.keys.clone();
        if file.format == "toml" && !keys.iter().any(|k| k == REASONING_DISPLAY_KEY) {
            keys.push(REASONING_DISPLAY_KEY.into());
        }
        files.push(ManagedFile {
            keys,
            original: before,
            expected: Some(after),
            ..file.clone()
        });
    }
    if !files.iter().any(|f| f.format == "toml") {
        return Err(AppError::new(
            "invalid_state",
            "当前 Codex 配置缺少管理记录，请重新使用供应商后重试",
        ));
    }
    if !files.iter().any(|f| f.format == "catalog") {
        // Early releases did not record the optional model catalog at all.
        // Include its first ownership in this same Fast transaction.
        let directory = files
            .iter()
            .find(|f| f.format == "toml")
            .unwrap()
            .path
            .parent()
            .unwrap();
        let path = directory.join("uni-switch-models.json");
        let original = read(&path)?;
        let mut catalog = match &original {
            Some(text) => parse_json(Some(text), &path)?,
            None => model_catalog(provider),
        };
        crate::model_capabilities::repair_catalog(&mut catalog, provider);
        repair_reasoning_catalog(&mut catalog)?;
        for model in catalog["models"].as_array_mut().unwrap() {
            model["additional_speed_tiers"] = if enabled { json!(["fast"]) } else { json!([]) };
            model["service_tiers"] = if enabled {
                json!([{"id":"priority", "name":"Fast", "description":"供应商优先服务档位"}])
            } else {
                json!([])
            };
        }
        files.push(ManagedFile {
            path,
            format: "catalog".into(),
            keys: vec![],
            original,
            expected: Some(json_text(&catalog)),
        });
    }
    Ok(files)
}

/// Model refreshes retain valid choices made in Codex itself. Explicitly
/// changing the default in uni-switch still updates the configured model.
pub fn preserve_codex_choices(
    file: &ManagedFile,
    provider: &StoredProvider,
    keep_model: bool,
) -> Result<Option<String>> {
    let mut after = parse_toml(file.expected.as_deref(), &file.path)?;
    let before = parse_toml(file.original.as_deref(), &file.path)?;
    for key in ["model", "model_reasoning_effort", "service_tier"] {
        if key == "model"
            && (!keep_model
                || !before.get(key).and_then(Item::as_str).is_some_and(|id| {
                    provider
                        .summary
                        .codex_options
                        .models
                        .iter()
                        .any(|m| m.enabled && m.id == id)
                }))
        {
            continue;
        }
        if let Some(value) = before.get(key) {
            after.as_table_mut().insert(key, value.clone());
        }
    }
    Ok(Some(after.to_string()))
}

/// Extend the displayed presets while preserving model metadata and its default.
/// This is a client menu repair; it does not establish gateway/model support.
pub fn repair_reasoning_catalog(catalog: &mut Value) -> Result<()> {
    let models = catalog
        .get_mut("models")
        .and_then(Value::as_array_mut)
        .filter(|models| !models.is_empty())
        .ok_or_else(|| {
            AppError::new(
                "invalid_catalog",
                "模型目录为空或格式无效，请重新同步模型并应用后重试",
            )
        })?;
    for model in models {
        let model = model
            .as_object_mut()
            .ok_or_else(|| AppError::new("invalid_catalog", "模型目录格式无效"))?;
        let levels = model
            .entry("supported_reasoning_levels")
            .or_insert_with(|| json!([]));
        let levels = levels
            .as_array_mut()
            .ok_or_else(|| AppError::new("invalid_catalog", "模型思考强度列表格式无效"))?;
        for (effort, description) in [
            ("none", "无"),
            ("minimal", "极低"),
            ("low", "低"),
            ("medium", "中"),
            ("high", "高"),
            ("xhigh", "更高"),
            ("max", "最大"),
            ("ultra", "最高"),
        ] {
            if !levels
                .iter()
                .any(|level| level.get("effort").and_then(Value::as_str) == Some(effort))
            {
                levels.push(json!({"effort": effort, "description": description}));
            }
        }
    }
    Ok(())
}

/// Codex Desktop filters model presets through this setting, whose default
/// omits max. Repair the display allowlist without selecting an effort.
pub fn repair_desktop_reasoning_display(doc: &mut DocumentMut) -> Result<()> {
    let mut efforts = match toml_item(doc, REASONING_DISPLAY_KEY) {
        Some(item) => item
            .as_array()
            .and_then(|array| {
                array
                    .iter()
                    .map(|value| {
                        value
                            .as_str()
                            .filter(|effort| {
                                crate::types::valid_effort(effort) || *effort == "persistent"
                            })
                            .map(str::to_owned)
                    })
                    .collect::<Option<Vec<_>>>()
            })
            .ok_or_else(|| {
                AppError::new(
                    "invalid_reasoning_display",
                    "Codex 的思考强度显示设置格式无效，请先修正 desktop.enabled-reasoning-efforts",
                )
            })?,
        None => vec!["low", "medium", "high", "xhigh", "ultra", "persistent"]
            .into_iter()
            .map(str::to_owned)
            .collect(),
    };
    for effort in [
        "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra",
    ] {
        if !efforts.iter().any(|e| e == effort) {
            efforts.push(effort.into());
        }
    }
    if doc.get("desktop").is_none() {
        doc["desktop"] = Item::Table(Table::new());
    }
    let desktop = doc
        .get_mut("desktop")
        .and_then(Item::as_table_like_mut)
        .ok_or_else(|| AppError::new("invalid_shape", "Codex 的 desktop 设置必须是 TOML 表"))?;
    let existing = desktop
        .get("enabled-reasoning-efforts")
        .and_then(Item::as_array);
    if existing.is_some_and(|array| {
        array
            .iter()
            .filter_map(toml_edit::Value::as_str)
            .eq(efforts.iter().map(String::as_str))
    }) {
        return Ok(());
    }
    let mut array = toml_edit::Array::new();
    for effort in efforts {
        array.push(effort);
    }
    desktop.insert("enabled-reasoning-efforts", toml_edit::value(array));
    Ok(())
}

/// Repair our catalog and Desktop's effort display allowlist. Leave the current model, effort,
/// credentials, context limits and unrelated settings byte-for-byte where possible.
pub fn reasoning_repair_files(
    directory: &Path,
    provider: &StoredProvider,
) -> Result<Vec<ManagedFile>> {
    let catalog_path = directory.join("uni-switch-models.json");
    let config_path = directory.join("config.toml");
    let before = read(&catalog_path)?;
    let mut catalog = match &before {
        Some(text) => parse_json(Some(text), &catalog_path)?,
        None => model_catalog(provider),
    };
    crate::model_capabilities::repair_catalog(&mut catalog, provider);
    repair_reasoning_catalog(&mut catalog)?;
    let config_before = read(&config_path)?;
    let mut config = parse_toml(config_before.as_deref(), &config_path)?;
    repair_desktop_reasoning_display(&mut config)?;
    let path = catalog_path.to_string_lossy();
    if config.get("model_catalog_json").and_then(Item::as_str) != Some(path.as_ref()) {
        config["model_catalog_json"] = toml_edit::value(path.as_ref());
    }
    let config_after = Some(config.to_string());
    let mut files = vec![ManagedFile {
        path: catalog_path,
        format: "catalog".into(),
        keys: vec![],
        original: before,
        expected: Some(json_text(&catalog)),
    }];
    if config_after != config_before {
        files.push(ManagedFile {
            path: config_path,
            format: "toml".into(),
            keys: vec!["model_catalog_json".into(), REASONING_DISPLAY_KEY.into()],
            original: config_before,
            expected: config_after,
        });
    }
    Ok(files)
}

fn claude_client_base_url(base_url: &str) -> &str {
    let base = base_url.trim_end_matches('/');
    base.strip_suffix("/v1").unwrap_or(base)
}

pub fn claude_client_base_needs_update(files: &[ManagedFile]) -> Result<bool> {
    for file in files {
        let key = ["env.ANTHROPIC_BASE_URL", "inferenceGatewayBaseUrl"]
            .into_iter()
            .find(|key| file.keys.iter().any(|managed| managed == key));
        if let Some(key) = key {
            let doc = parse_json(file.expected.as_deref(), &file.path)?;
            if get_json(&doc, key)
                .and_then(Value::as_str)
                .is_some_and(|base| claude_client_base_url(base) != base)
            {
                return Ok(true);
            }
        }
    }
    Ok(false)
}

fn desktop_model_valid(model: &str) -> bool {
    let tail = model.strip_prefix("anthropic/").unwrap_or(model);
    [
        "claude-sonnet-",
        "claude-opus-",
        "claude-haiku-",
        "claude-fable-",
    ]
    .iter()
    .any(|prefix| tail.starts_with(prefix) && tail.len() > prefix.len())
}

fn get_json<'a>(doc: &'a Value, key: &str) -> Option<&'a Value> {
    key.split('.').try_fold(doc, |value, key| value.get(key))
}

fn set_json(doc: &mut Value, key: &str, value: Option<Value>) -> Result<()> {
    let parts: Vec<_> = key.split('.').collect();
    let mut parent = doc;
    for part in &parts[..parts.len() - 1] {
        let obj = parent.as_object_mut().ok_or_else(|| {
            AppError::new("invalid_shape", format!("配置字段 {key} 的父级必须是对象"))
        })?;
        if !obj.contains_key(*part) {
            if value.is_none() {
                return Ok(());
            }
            obj.insert((*part).into(), json!({}));
        }
        parent = obj.get_mut(*part).unwrap();
    }
    let obj = parent.as_object_mut().ok_or_else(|| {
        AppError::new("invalid_shape", format!("配置字段 {key} 的父级必须是对象"))
    })?;
    if let Some(value) = value {
        obj.insert(parts.last().unwrap().to_string(), value);
    } else {
        obj.remove(*parts.last().unwrap());
    }
    Ok(())
}
fn remove_json(doc: &mut Value, key: &str) -> Result<()> {
    set_json(doc, key, None)
}

fn toml_item<'a>(doc: &'a DocumentMut, key: &str) -> Option<&'a Item> {
    let mut parts = key.split('.');
    let mut item = doc.get(parts.next()?)?;
    for part in parts {
        item = item.get(part)?;
    }
    Some(item)
}

pub fn projection(file: &ManagedFile, content: Option<&str>) -> Result<Value> {
    if file.format == "catalog" {
        return content
            .map(|text| parse_json(Some(text), &file.path))
            .unwrap_or(Ok(Value::Null));
    }
    if file.format == "toml" {
        let doc = parse_toml(content, &file.path)?;
        let map: serde_json::Map<_, _> = file
            .keys
            .iter()
            .map(|key| {
                (
                    key.clone(),
                    toml_item(&doc, key)
                        .map(|item| json!(item.to_string()))
                        .unwrap_or(Value::Null),
                )
            })
            .collect();
        Ok(Value::Object(map))
    } else {
        let doc = parse_json(content, &file.path)?;
        if file.format == "meta" {
            let entry = doc
                .get("entries")
                .and_then(Value::as_array)
                .and_then(|entries| {
                    entries
                        .iter()
                        .find(|e| e.get("id").and_then(Value::as_str) == Some(PROFILE_UUID))
                })
                .cloned()
                .unwrap_or(Value::Null);
            Ok(json!({"appliedId": doc.get("appliedId"), "entry": entry}))
        } else {
            Ok(Value::Object(
                file.keys
                    .iter()
                    .map(|key| {
                        (
                            key.clone(),
                            get_json(&doc, key).cloned().unwrap_or(Value::Null),
                        )
                    })
                    .collect(),
            ))
        }
    }
}

pub fn changed(file: &ManagedFile) -> Result<bool> {
    Ok(projection(file, read(&file.path)?.as_deref())?
        != projection(file, file.expected.as_deref())?)
}

/// Accept only missing standard presets. Changes to descriptions, custom
/// presets, ordering, model metadata or routing still remain conflicts.
fn fill_missing_reasoning_presets(current: &mut Value, expected: &Value) {
    let Some(models) = current.get_mut("models").and_then(Value::as_array_mut) else {
        return;
    };
    let Some(old_models) = expected.get("models").and_then(Value::as_array) else {
        return;
    };
    for (model, old) in models.iter_mut().zip(old_models) {
        let Some(levels) = old
            .get("supported_reasoning_levels")
            .and_then(Value::as_array)
        else {
            continue;
        };
        let present = model
            .get("supported_reasoning_levels")
            .cloned()
            .unwrap_or(json!([]));
        let Some(present_levels) = present.as_array() else {
            continue;
        };
        let retained: Vec<_> = levels
            .iter()
            .filter(|level| {
                !level
                    .get("effort")
                    .and_then(Value::as_str)
                    .is_some_and(crate::types::valid_effort)
                    || present_levels
                        .iter()
                        .any(|entry| entry.get("effort") == level.get("effort"))
            })
            .cloned()
            .collect();
        if *present_levels == retained && model.is_object() {
            model["supported_reasoning_levels"] = json!(levels);
        }
    }
}

pub fn changed_for_provider(file: &ManagedFile, provider: Option<&StoredProvider>) -> Result<bool> {
    if !changed(file)? {
        return Ok(false);
    }
    let Some(_provider) = provider.filter(|p| p.summary.family == crate::types::Family::Codex)
    else {
        return Ok(true);
    };
    if file.format == "catalog" {
        let current = read(&file.path)?;
        // A missing whole catalog is still a conflict; only preset omissions
        // inside a valid managed catalog can be repaired automatically.
        let (Some(current), Some(expected)) = (current, file.expected.as_deref()) else {
            return Ok(true);
        };
        let mut current = parse_json(Some(&current), &file.path)?;
        let expected = parse_json(Some(expected), &file.path)?;
        fill_missing_reasoning_presets(&mut current, &expected);
        return Ok(current != expected);
    }
    if file.format != "toml" {
        return Ok(true);
    }
    let current = read(&file.path)?;
    let doc = parse_toml(current.as_deref(), &file.path)?;
    let model = doc.get("model").and_then(Item::as_str).unwrap_or("");
    let catalog_path = file.path.parent().unwrap().join("uni-switch-models.json");
    let expected = parse_toml(file.expected.as_deref(), &file.path)?;
    let applied_catalog = expected
        .get("model_catalog_json")
        .and_then(Item::as_str)
        .is_some_and(|path| Path::new(path) == catalog_path);
    let included = if applied_catalog {
        read(&catalog_path)?
            .map(|text| parse_json(Some(&text), &catalog_path))
            .transpose()?
            .and_then(|catalog| {
                catalog
                    .get("models")
                    .and_then(Value::as_array)
                    .map(|models| {
                        models
                            .iter()
                            .any(|m| m.get("slug").and_then(Value::as_str) == Some(model))
                    })
            })
            .unwrap_or(false)
    } else {
        false
    };
    if !included {
        return Ok(true);
    }
    if doc
        .get("model_reasoning_effort")
        .is_some_and(|v| !v.as_str().is_some_and(crate::types::valid_effort))
        || doc.get("service_tier").is_some_and(|v| {
            !v.as_str()
                .is_some_and(|s| matches!(s, "default" | "priority" | "fast" | "flex"))
        })
    {
        return Ok(true);
    }
    // Codex's model menu can persist these choices without changing the supplier.
    // The endpoint, credential, catalog, context and other managed fields still
    // have to match exactly before these client choices can be accepted.
    let mut routing = file.clone();
    routing.keys.retain(|key| {
        ![
            "model",
            "model_reasoning_effort",
            "service_tier",
            REASONING_DISPLAY_KEY,
        ]
        .contains(&key.as_str())
    });
    Ok(
        projection(&routing, current.as_deref())?
            != projection(&routing, file.expected.as_deref())?,
    )
}

pub fn restore(file: &ManagedFile) -> Result<Change> {
    let current = read(&file.path)?;
    if projection(file, current.as_deref())? != projection(file, file.expected.as_deref())? {
        return Err(AppError::new(
            "external_change",
            format!(
                "{} 中的 API 字段已被其他工具修改，请先撤回相应修改后再重试恢复",
                file.path.display()
            ),
        ));
    }
    let after = if file.format == "catalog" {
        file.original.clone()
    } else if file.format == "toml" {
        let mut doc = parse_toml(current.as_deref(), &file.path)?;
        let original = parse_toml(file.original.as_deref(), &file.path)?;
        for key in &file.keys {
            let value = toml_item(&original, key).cloned();
            if let Some((parent, child)) = key.split_once('.') {
                if let Some(table) = doc.get_mut(parent).and_then(Item::as_table_like_mut) {
                    if let Some(value) = value {
                        table.insert(child, value);
                    } else {
                        table.remove(child);
                    }
                }
            } else if let Some(value) = value {
                doc.as_table_mut().insert(key, value);
            } else {
                doc.as_table_mut().remove(key);
            }
        }
        if doc
            .get("model_providers")
            .and_then(Item::as_table_like)
            .is_some_and(|t| t.is_empty())
        {
            doc.as_table_mut().remove("model_providers");
        }
        if doc
            .get("desktop")
            .and_then(Item::as_table_like)
            .is_some_and(|table| table.is_empty())
        {
            doc.as_table_mut().remove("desktop");
        }
        let text = doc.to_string();
        if file.original.is_none() && doc.as_table().is_empty() {
            None
        } else {
            Some(text)
        }
    } else {
        let mut doc = parse_json(current.as_deref(), &file.path)?;
        let original = parse_json(file.original.as_deref(), &file.path)?;
        if file.format == "meta" {
            let obj = doc.as_object_mut().unwrap();
            let entries = obj
                .entry("entries")
                .or_insert(json!([]))
                .as_array_mut()
                .ok_or_else(|| {
                    AppError::new("invalid_shape", "Claude 配置库 entries 必须是数组")
                })?;
            entries.retain(|entry| entry.get("id").and_then(Value::as_str) != Some(PROFILE_UUID));
            if let Some(ours) = original
                .get("entries")
                .and_then(Value::as_array)
                .and_then(|a| {
                    a.iter()
                        .find(|e| e.get("id").and_then(Value::as_str) == Some(PROFILE_UUID))
                })
            {
                entries.push(ours.clone());
            }
            match original.get("appliedId") {
                Some(id) => {
                    obj.insert("appliedId".into(), id.clone());
                }
                None => {
                    obj.remove("appliedId");
                }
            }
            if original.get("entries").is_none()
                && obj
                    .get("entries")
                    .and_then(Value::as_array)
                    .is_some_and(|e| e.is_empty())
            {
                obj.remove("entries");
            }
        } else {
            for key in &file.keys {
                set_json(&mut doc, key, get_json(&original, key).cloned())?;
            }
            if original.get("env").is_none()
                && doc
                    .get("env")
                    .and_then(Value::as_object)
                    .is_some_and(|env| env.is_empty())
            {
                doc.as_object_mut().unwrap().remove("env");
            }
        }
        if file.original.is_none() && doc.as_object().unwrap().is_empty() {
            None
        } else {
            Some(json_text(&doc))
        }
    };
    Ok(Change {
        path: file.path.clone(),
        before: current,
        after,
    })
}

/// Read the model choices in the managed global config, not an active thread
/// or upstream routing result. Never return credentials or inspect sessions.
pub(crate) struct CodexModelSelection {
    pub model: String,
    pub reasoning_effort: Option<String>,
}

pub(crate) fn codex_model_selection(directory: &Path) -> Result<Option<CodexModelSelection>> {
    let path = directory.join("config.toml");
    let Some(text) = read(&path)? else {
        return Ok(None);
    };
    let doc = parse_toml(Some(&text), &path)?;
    if doc.get("model_provider").and_then(Item::as_str) != Some("uni_switch") {
        return Ok(None);
    }
    let Some(model) = doc
        .get("model")
        .and_then(Item::as_str)
        .filter(|value| !value.is_empty())
    else {
        return Ok(None);
    };
    Ok(Some(CodexModelSelection {
        model: model.into(),
        reasoning_effort: doc
            .get("model_reasoning_effort")
            .and_then(Item::as_str)
            .filter(|value| crate::types::valid_effort(value))
            .map(str::to_owned),
    }))
}

/// Read only model choices for upgrading an existing managed snapshot.
/// Does not extract credentials or create/adopt a supplier.
pub(crate) struct ClientModelPreferences {
    pub model: String,
    pub reasoning_effort: Option<String>,
    pub codex_options: crate::types::CodexOptions,
}

pub(crate) fn read_model_preferences(
    target: Target,
    directory: &Path,
) -> Result<ClientModelPreferences> {
    let files = specifications(target, directory);
    let path = if target == Target::ClaudeDesktop {
        let meta = parse_json(read(&files[3].0)?.as_deref(), &files[3].0)?;
        let id = meta
            .get("appliedId")
            .and_then(Value::as_str)
            .ok_or_else(|| {
                AppError::new("configuration_missing", "Claude 桌面端没有生效的第三方配置")
            })?;
        if uuid::Uuid::try_parse(id).is_err() {
            return Err(AppError::new(
                "invalid_profile",
                "Claude profile ID 必须是 UUID",
            ));
        }
        files[3].0.parent().unwrap().join(format!("{id}.json"))
    } else {
        files[0].0.clone()
    };
    let text =
        read(&path)?.ok_or_else(|| AppError::new("configuration_missing", "目标配置文件不存在"))?;
    let mut codex_options = crate::types::CodexOptions::default();
    let (model, reasoning_effort) = match target {
        Target::Codex => {
            let doc = parse_toml(Some(&text), &path)?;
            codex_options.fast_mode = doc
                .get("service_tier")
                .and_then(Item::as_str)
                .map(|v| matches!(v, "priority" | "fast"));
            codex_options.context_window =
                doc.get("model_context_window").and_then(Item::as_integer);
            codex_options.auto_compact_token_limit = doc
                .get("model_auto_compact_token_limit")
                .and_then(Item::as_integer);
            // An arbitrary catalog path must never trigger unrelated file reads.
            let catalog_path = directory.join("uni-switch-models.json");
            if doc
                .get("model_catalog_json")
                .and_then(Item::as_str)
                .is_some_and(|p| Path::new(p) == catalog_path)
            {
                if let Some(catalog) = read(&catalog_path)? {
                    codex_options.models =
                        crate::supplier::parse_models(&parse_json(Some(&catalog), &catalog_path)?)?;
                    for model in &mut codex_options.models {
                        model.capabilities = Default::default();
                    }
                }
            }
            (
                doc.get("model").and_then(Item::as_str).unwrap_or("").into(),
                doc.get("model_reasoning_effort")
                    .and_then(Item::as_str)
                    .map(str::to_owned),
            )
        }
        Target::ClaudeDesktop => {
            let doc = parse_json(Some(&text), &path)?;
            if let Some(models) = doc.get("inferenceModels").and_then(Value::as_array) {
                codex_options.models = models
                    .iter()
                    .filter_map(|m| m.as_str().or_else(|| m.get("name").and_then(Value::as_str)))
                    .map(|id| crate::types::ProviderModel {
                        id: id.into(),
                        enabled: true,
                        context_window: None,
                        reasoning_efforts: Vec::new(),
                        ..Default::default()
                    })
                    .collect();
            }
            (
                codex_options
                    .models
                    .first()
                    .map(|m| m.id.clone())
                    .unwrap_or_default(),
                None,
            )
        }
        Target::ClaudeCli => {
            let doc = parse_json(Some(&text), &path)?;
            let model = doc
                .get("env")
                .and_then(|env| env.get("ANTHROPIC_MODEL"))
                .or_else(|| doc.get("model"))
                .and_then(Value::as_str)
                .unwrap_or("");
            (model.into(), None)
        }
    };
    Ok(ClientModelPreferences {
        model,
        reasoning_effort,
        codex_options,
    })
}
