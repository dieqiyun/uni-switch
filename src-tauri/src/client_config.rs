use crate::{
    adapters::Change,
    error::{AppError, Result},
    types::{CodexProtocol, ProviderModel, StoredProvider, Target},
};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::path::{Path, PathBuf};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ClientKind {
    Codex,
    ClaudeDesktop,
    ClaudeCli,
    Zcode,
    Dsh,
    Workbuddy,
}
impl ClientKind {
    pub fn id(self) -> &'static str {
        match self {
            Self::Codex => "codex",
            Self::ClaudeDesktop => "claude_desktop",
            Self::ClaudeCli => "claude_cli",
            Self::Zcode => "zcode",
            Self::Dsh => "dsh",
            Self::Workbuddy => "workbuddy",
        }
    }
    pub fn target(self) -> Option<Target> {
        match self {
            Self::Codex => Some(Target::Codex),
            Self::ClaudeDesktop => Some(Target::ClaudeDesktop),
            Self::ClaudeCli => Some(Target::ClaudeCli),
            _ => None,
        }
    }
}
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum NativeProtocol {
    Messages,
    ChatCompletions,
    Responses,
}
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ConfigFile {
    pub id: String,
    pub path: String,
    pub format: String,
    pub exists: bool,
}
#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ClientConfigStatus {
    pub client: ClientKind,
    pub directory: String,
    pub files: Vec<ConfigFile>,
    pub active_provider_id: Option<String>,
    pub can_restore: bool,
    pub state: String,
    pub message: String,
    pub revision: String,
}
#[derive(Debug, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ConfigDocument {
    pub client: ClientKind,
    pub file_id: String,
    pub path: String,
    pub format: String,
    pub content: String,
    pub revision: String,
    pub exists: bool,
}
#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ConfigWriteResult {
    pub changed: bool,
    pub backup_path: Option<String>,
    pub configuration_revision: u64,
}

pub fn default_directory(client: ClientKind) -> Result<PathBuf> {
    if let Some(target) = client.target() {
        return crate::adapters::default_directory(target);
    }
    let home = dirs::home_dir().ok_or_else(|| AppError::new("missing_home", "无法确定用户目录"))?;
    let absolute_env = |key| {
        std::env::var_os(key)
            .map(PathBuf::from)
            .filter(|p| p.is_absolute())
    };
    Ok(match client {
        ClientKind::Zcode => {
            if let Some(file) = absolute_env("ZCODE_PERSONAL_PROVIDER_CONFIG_FILE") {
                return file
                    .parent()
                    .map(Path::to_path_buf)
                    .ok_or_else(|| AppError::new("invalid_path", "ZCode 配置路径无效"));
            }
            absolute_env("ZCODE_DATA_BASE_DIR")
                .unwrap_or(home)
                .join(".zcode/v2")
        }
        ClientKind::Dsh => absolute_env("DSH_HOME").unwrap_or_else(|| home.join(".dsh")),
        ClientKind::Workbuddy => home.join(".workbuddy"),
        _ => unreachable!(),
    })
}

/// Only these files are exposed to IPC. No arbitrary caller-supplied paths.
pub fn specifications(
    client: ClientKind,
    directory: &Path,
    default_binding: bool,
) -> Result<Vec<(PathBuf, &'static str)>> {
    let mut files = if let Some(target) = client.target() {
        crate::adapters::specifications(target, directory)
            .into_iter()
            .map(|(p, f, _)| (p, if f == "toml" { "toml" } else { "json" }))
            .collect::<Vec<_>>()
    } else {
        match client {
            ClientKind::Zcode => vec![(
                if default_binding {
                    std::env::var_os("ZCODE_PERSONAL_PROVIDER_CONFIG_FILE")
                        .map(PathBuf::from)
                        .filter(|p| p.is_absolute())
                        .unwrap_or_else(|| directory.join("provider_config.json"))
                } else {
                    directory.join("provider_config.json")
                },
                "json",
            )],
            ClientKind::Dsh => vec![
                (directory.join("cordis.patch.yml"), "yaml"),
                (directory.join(".credentials.yaml"), "yaml"),
            ],
            ClientKind::Workbuddy => vec![(directory.join("models.json"), "json")],
            _ => unreachable!(),
        }
    };
    if client == ClientKind::Codex {
        files.push((directory.join("auth.json"), "json"));
    }
    if client == ClientKind::ClaudeDesktop {
        let meta_paths: Vec<_> = files
            .iter()
            .filter(|(p, _)| p.file_name().is_some_and(|n| n == "_meta.json"))
            .map(|(p, _)| p.clone())
            .collect();
        for meta in meta_paths {
            check_path(&meta)?;
            if let Some(text) = crate::adapters::read(&meta)? {
                if let Ok(doc) = serde_json::from_str::<Value>(&text) {
                    if let Some(id) = doc.get("appliedId").and_then(Value::as_str) {
                        if uuid::Uuid::parse_str(id).is_ok() {
                            let path = meta.parent().unwrap().join(format!("{id}.json"));
                            if !files.iter().any(|(p, _)| *p == path) {
                                files.push((path, "json"));
                            }
                        }
                    }
                }
            }
        }
    }
    Ok(files)
}

pub fn check_path(path: &Path) -> Result<()> {
    for ancestor in path.ancestors() {
        if std::fs::symlink_metadata(ancestor).is_ok_and(|m| {
            #[cfg(windows)]
            {
                use std::os::windows::fs::MetadataExt;
                m.file_attributes() & 0x400 != 0
            }
            #[cfg(not(windows))]
            {
                m.file_type().is_symlink()
            }
        }) {
            return Err(AppError::new(
                "symlink",
                "配置路径包含符号链接或重解析点，请选择实际目录",
            ));
        }
    }
    if path.is_dir() {
        return Err(AppError::new("invalid_path", "配置文件路径被目录占用"));
    }
    Ok(())
}

pub fn revision(path: &Path, content: Option<&str>) -> String {
    use sha2::{Digest, Sha256};
    let mut hash = Sha256::new();
    hash.update(path.to_string_lossy().as_bytes());
    hash.update([0]);
    hash.update([u8::from(content.is_some())]);
    if let Some(text) = content {
        hash.update(text.as_bytes());
    }
    format!("{:x}", hash.finalize())
}
pub fn validate_text(format: &str, text: &str) -> Result<()> {
    if text.len() > 4 * 1024 * 1024 || text.contains('\0') {
        return Err(AppError::new(
            "invalid_config",
            "配置文件超过 4 MiB 或包含无效字符",
        ));
    }
    match format {
        "json" => {
            json_object(Some(text))?;
        }
        "toml" => {
            text.parse::<toml_edit::DocumentMut>()
                .map_err(|_| AppError::new("invalid_toml", "TOML 格式无效，请检查语法后保存"))?;
        }
        "yaml" => {
            yaml(text)?;
        }
        _ => return Err(AppError::new("invalid_format", "不支持此配置格式")),
    }
    Ok(())
}
fn json_object(text: Option<&str>) -> Result<Value> {
    let value: Value = serde_json::from_str(text.unwrap_or("{}")).map_err(|e| {
        AppError::new(
            "invalid_json",
            format!("JSON 格式无效（第 {} 行，第 {} 列）", e.line(), e.column()),
        )
    })?;
    if !value.is_object() {
        return Err(AppError::new("invalid_json", "JSON 顶层必须是对象"));
    }
    Ok(value)
}
fn yaml(text: &str) -> Result<serde_yaml_ng::Value> {
    serde_yaml_ng::from_str(text).map_err(|e| {
        let location = e
            .location()
            .map(|l| format!("（第 {} 行，第 {} 列）", l.line(), l.column()))
            .unwrap_or_default();
        AppError::new(
            "invalid_yaml",
            format!("YAML 格式无效{location}，请检查后保存"),
        )
    })
}
fn pretty(value: &Value) -> String {
    format!(
        "{}\n",
        serde_json::to_string_pretty(value).expect("JSON value")
    )
}
fn enabled_models(provider: &StoredProvider) -> Result<Vec<ProviderModel>> {
    let mut models: Vec<_> = provider
        .summary
        .codex_options
        .models
        .iter()
        .filter(|m| m.enabled)
        .cloned()
        .collect();
    if models.is_empty() && !provider.summary.codex_options.models.is_empty() {
        return Err(AppError::new("no_models", "请先为供应商启用至少一个模型"));
    }
    if !models.iter().any(|m| m.id == provider.summary.model) {
        models.insert(
            0,
            ProviderModel {
                id: provider.summary.model.clone(),
                enabled: true,
                ..Default::default()
            },
        );
    }
    Ok(models)
}
fn endpoint_support(model: &ProviderModel, protocol: NativeProtocol) -> Option<bool> {
    let field = |p: &crate::types::ModelProfile| match protocol {
        NativeProtocol::Messages => p.endpoints.messages,
        NativeProtocol::ChatCompletions => p.endpoints.chat_completions,
        NativeProtocol::Responses => p.endpoints.responses,
    };
    field(&model.profile_overrides)
        .or_else(|| field(&model.profile))
        .or_else(|| field(&model.official_profile))
}
fn ensure_protocol(
    client: ClientKind,
    provider: &StoredProvider,
    protocol: NativeProtocol,
    models: &[ProviderModel],
) -> Result<()> {
    if client == ClientKind::Workbuddy && protocol != NativeProtocol::ChatCompletions {
        return Err(AppError::new(
            "unsupported_protocol",
            "WorkBuddy 自定义模型请使用 OpenAI Chat Completions 协议",
        ));
    }
    if client == ClientKind::Workbuddy && provider.summary.auth_mode != "bearer" {
        return Err(AppError::new(
            "unsupported_auth",
            "WorkBuddy 一键配置暂不支持 x-api-key 认证；请使用支持 Bearer 的供应商",
        ));
    }
    let same_family = (protocol == NativeProtocol::Messages)
        == (provider.summary.upstream_protocol() == CodexProtocol::Anthropic);
    for model in models {
        let support = endpoint_support(model, protocol);
        if support == Some(false) || (!same_family && support != Some(true)) {
            return Err(AppError::new(
                "unsupported_protocol",
                "所选模型没有此接口的支持依据，请同步模型、核实端点能力或选择兼容供应商",
            ));
        }
    }
    if provider.api_key.is_empty() {
        return Err(AppError::new("missing_key", "供应商缺少 API Key"));
    }
    Ok(())
}
fn array<'a>(value: &'a mut Value, key: &str) -> Result<&'a mut Vec<Value>> {
    let obj = value
        .as_object_mut()
        .ok_or_else(|| AppError::new("invalid_config", "配置字段必须是对象"))?;
    obj.entry(key)
        .or_insert_with(|| json!([]))
        .as_array_mut()
        .ok_or_else(|| AppError::new("invalid_config", "模型或供应商列表格式无效"))
}
fn object<'a>(value: &'a mut Value, key: &str) -> Result<&'a mut Value> {
    let obj = value
        .as_object_mut()
        .ok_or_else(|| AppError::new("invalid_config", "配置字段必须是对象"))?;
    let field = obj.entry(key).or_insert_with(|| json!({}));
    if !field.is_object() {
        return Err(AppError::new("invalid_config", "配置字段必须是对象"));
    }
    Ok(field)
}
// OpenAI SDKs append only the resource, while Anthropic SDKs own /v1.
// A bare origin uses the standard OpenAI /v1 default. Explicit API prefixes
// and full resource URLs remain authoritative (e.g. /api/paas/v4).
fn base(provider: &StoredProvider, protocol: NativeProtocol) -> Result<String> {
    let mut url = crate::types::validate_url(&provider.summary.base_url)?;
    let path = url.path().trim_end_matches('/');
    let resource_base = ["/chat/completions", "/responses", "/messages"]
        .iter()
        .find_map(|suffix| path.strip_suffix(suffix));
    let api_path = resource_base.unwrap_or(path);
    let api_path =
        if protocol != NativeProtocol::Messages && api_path.is_empty() && resource_base.is_none() {
            "/v1"
        } else {
            api_path
        };
    let api_path = api_path.to_owned();
    url.set_path(&api_path);
    Ok(url.as_str().trim_end_matches('/').to_owned())
}
fn headers(provider: &StoredProvider, protocol: NativeProtocol) -> Value {
    match provider.summary.auth_mode.as_str() {
        "x-api-key" => json!({"x-api-key": provider.api_key}),
        "bearer" if protocol == NativeProtocol::Messages => {
            json!({"Authorization":format!("Bearer {}", provider.api_key)})
        }
        _ => json!({}),
    }
}

pub fn plan(
    client: ClientKind,
    specs: &[(PathBuf, &'static str)],
    provider: &StoredProvider,
    protocol: NativeProtocol,
) -> Result<Vec<Change>> {
    let models = enabled_models(provider)?;
    ensure_protocol(client, provider, protocol, &models)?;
    let api_base = base(provider, protocol)?;
    let before: Vec<_> = specs
        .iter()
        .map(|(path, _)| {
            check_path(path)?;
            crate::adapters::read(path)
        })
        .collect::<Result<_>>()?;
    let output = match client {
        ClientKind::Zcode => {
            let mut doc = json_object(before[0].as_deref())?;
            if before[0].is_some() && doc.get("schemaVersion") != Some(&json!(1)) {
                return Err(AppError::new(
                    "unsupported_schema",
                    "ZCode 个人配置只支持 schemaVersion 1，请先检查客户端配置版本",
                ));
            }
            doc["schemaVersion"] = json!(1);
            let config = object(&mut doc, "config")?;
            let rules = array(object(config, "providerConfigRules")?, "providerRules")?;
            let ids: Vec<_> = models.iter().map(|m| m.id.clone()).collect();
            let api_type = match protocol {
                NativeProtocol::Messages => "anthropic-messages",
                NativeProtocol::ChatCompletions => "openai-chat-completions",
                NativeProtocol::Responses => "openai-responses",
            };
            let rule = json!({"providerId":"uni-switch", "providerName":provider.summary.name,"enabled":true,"config":{"group":"standard-personal","access":{"type":"api-key","apiKey":provider.api_key},"api":{"type":api_type,"baseUrl":api_base,"headers":headers(provider,protocol)},"personalModelIds":ids,"modelOrder":ids,"visibility":"visible"}});
            if let Some(index) = rules.iter().position(|r| r["providerId"] == "uni-switch") {
                rules[index] = rule;
            } else {
                rules.push(rule);
            }
            let order = array(config, "providerOrder")?;
            if !order.iter().any(|v| v == "uni-switch") {
                order.insert(0, json!("uni-switch"));
            }
            let model_rules = object(config, "modelConfigRules")?;
            array(model_rules, "providerModelRules")?;
            array(model_rules, "manualProviderModelRules")?;
            config["defaultModelSelection"] =
                json!({"providerId":"uni-switch","modelId":provider.summary.model});
            vec![pretty(&doc)]
        }
        ClientKind::Workbuddy => {
            let mut doc = json_object(before[0].as_deref())?;
            let entries = array(&mut doc, "models")?;
            let url = format!("{}/chat/completions", api_base);
            for model in &models {
                let mut entry = entries
                    .iter()
                    .find(|e| e["id"] == model.id)
                    .cloned()
                    .unwrap_or_else(|| json!({}));
                if !entry.is_object() {
                    return Err(AppError::new(
                        "invalid_config",
                        "WorkBuddy 模型条目必须是对象",
                    ));
                }
                entry["id"] = json!(model.id);
                entry["name"] = json!(format!("{} · {}", provider.summary.name, model.id));
                entry["vendor"] = json!("OpenAI");
                entry["apiKey"] = json!(provider.api_key);
                entry["url"] = json!(url);
                // Remove a prior Responses override when explicitly applying Chat.
                entry.as_object_mut().unwrap().remove("api");
                let input = model
                    .profile_overrides
                    .max_input_tokens
                    .or(model.profile.max_input_tokens)
                    .or(model.official_profile.max_input_tokens)
                    .or(model.profile_overrides.context_window)
                    .or(model.context_window)
                    .or(model.official_profile.context_window);
                let output = model
                    .profile_overrides
                    .max_output_tokens
                    .or(model.profile.max_output_tokens)
                    .or(model.official_profile.max_output_tokens);
                if let Some(n) = input.filter(|n| *n > 0) {
                    entry["maxInputTokens"] = json!(n);
                }
                if let Some(n) = output.filter(|n| *n > 0) {
                    entry["maxOutputTokens"] = json!(n);
                }
                if let Some(v) = model
                    .profile_overrides
                    .tool_calls
                    .or(model.profile.tool_calls)
                    .or(model.official_profile.tool_calls)
                {
                    entry["supportsToolCall"] = json!(v);
                }
                if let Some(v) = model
                    .capability_overrides
                    .image_input
                    .or(model.capabilities.image_input)
                    .or(model.official_capabilities.image_input)
                {
                    entry["supportsImages"] = json!(v);
                }
                if let Some(index) = entries.iter().position(|e| e["id"] == model.id) {
                    entries[index] = entry;
                } else {
                    entries.push(entry);
                }
            }
            if doc.get("availableModels").is_some() {
                let visible = array(&mut doc, "availableModels")?;
                if !visible.is_empty() {
                    for model in &models {
                        if !visible.iter().any(|v| v == &model.id) {
                            visible.push(json!(model.id));
                        }
                    }
                }
            }
            vec![pretty(&doc)]
        }
        ClientKind::Dsh => {
            let original = before[0].as_deref().unwrap_or("");
            let mut stripped = remove_managed_block(original)?;
            if stripped.trim() == "[]" || stripped.trim() == "null" {
                stripped.clear();
            }
            let parsed = yaml(&stripped)?;
            if !parsed.is_null() && !parsed.is_sequence() {
                return Err(AppError::new(
                    "invalid_config",
                    "DSH cordis.patch.yml 顶层必须是补丁列表",
                ));
            }
            if stripped.contains("uni-switch-llm") || stripped.contains("UNI_SWITCH_DSH_API_KEY") {
                return Err(AppError::new(
                    "config_collision",
                    "DSH 已有同名 uni-switch 路由，请先在配置编辑器中检查",
                ));
            }
            let api = match protocol {
                NativeProtocol::Messages => "anthropic-messages",
                NativeProtocol::ChatCompletions => "openai-completions",
                NativeProtocol::Responses => "openai-responses",
            };
            let model_values: Vec<_> = models
                .iter()
                .map(|m| {
                    let mut v = json!({"id":m.id,"name":m.id});
                    if let Some(n) = m
                        .profile_overrides
                        .context_window
                        .or(m.context_window)
                        .or(m.profile.context_window)
                        .or(m.official_profile.context_window)
                        .filter(|n| *n > 0)
                    {
                        v["contextWindow"] = json!(n);
                    }
                    if let Some(n) = m
                        .profile_overrides
                        .max_output_tokens
                        .or(m.profile.max_output_tokens)
                        .or(m.official_profile.max_output_tokens)
                        .filter(|n| *n > 0)
                    {
                        v["maxTokens"] = json!(n);
                    }
                    if m.capability_overrides
                        .image_input
                        .or(m.capabilities.image_input)
                        .or(m.official_capabilities.image_input)
                        == Some(true)
                    {
                        v["input"] = json!(["text", "image"]);
                    }
                    v
                })
                .collect();
            let base_url = api_base;
            let base_url = if protocol == NativeProtocol::Messages {
                base_url.strip_suffix("/v1").unwrap_or(&base_url).to_owned()
            } else {
                base_url
            };
            let block = json!([{"insert":[{"id":"uni-switch-llm","name":"@deepseek-ai/dsh-llm-pi-ai","config":{"providers":{"uni-switch":{"displayName":provider.summary.name,"apiKeyEnv":"UNI_SWITCH_DSH_API_KEY","api":api,"baseURL":base_url,"headers":headers(provider,protocol),"models":model_values}}}}]}, {"id":"agent-default-model","config":{"provider":"uni-switch","model":provider.summary.model}}]);
            let generated = serde_yaml_ng::to_string(&block)
                .map_err(|_| AppError::new("serialization", "DSH 配置无法生成"))?;
            let patch = format!(
                "{}{}# uni-switch managed begin\n{}# uni-switch managed end\n",
                stripped,
                if stripped.is_empty() || stripped.ends_with('\n') {
                    ""
                } else {
                    "\n"
                },
                generated
            );
            let mut creds: Value = if let Some(text) = &before[1] {
                serde_yaml_ng::from_str(text)
                    .map_err(|_| AppError::new("invalid_config", "DSH 凭据文件格式无效"))?
            } else {
                json!({"version":1,"refs":{},"records":{}})
            };
            if creds.get("version") != Some(&json!(1))
                || !creds["refs"].is_object()
                || !creds["records"].is_object()
                || creds.as_object().is_none_or(|o| {
                    o.keys()
                        .any(|k| !matches!(k.as_str(), "version" | "refs" | "records"))
                })
            {
                return Err(AppError::new(
                    "unsupported_schema",
                    "DSH 凭据文件需要 version: 1、refs 和 records；旧格式请先手动转换",
                ));
            }
            creds["refs"]["UNI_SWITCH_DSH_API_KEY"] = json!(provider.api_key);
            // JSON is valid YAML and preserves all credential record values.
            vec![patch, pretty(&creds)]
        }
        _ => {
            return Err(AppError::new(
                "invalid_client",
                "此客户端请通过原有供应商入口配置",
            ))
        }
    };
    specs
        .iter()
        .zip(before)
        .zip(output)
        .map(|(((path, format), before), after)| {
            validate_text(format, &after)?;
            Ok(Change {
                path: path.clone(),
                before,
                after: Some(after),
            })
        })
        .collect()
}
fn remove_managed_block(text: &str) -> Result<String> {
    let begin = "# uni-switch managed begin";
    let end = "# uni-switch managed end";
    let starts: Vec<_> = text.match_indices(begin).collect();
    let ends: Vec<_> = text.match_indices(end).collect();
    match (starts.as_slice(), ends.as_slice()) {
        ([], []) => Ok(text.to_owned()),
        ([(start, _)], [(finish, _)])
            if start < finish && (*start == 0 || text.as_bytes()[start - 1] == b'\n') =>
        {
            let tail = finish + end.len();
            let tail = if text[tail..].starts_with("\r\n") {
                tail + 2
            } else if text[tail..].starts_with('\n') {
                tail + 1
            } else {
                tail
            };
            Ok(format!("{}{}", &text[..*start], &text[tail..]))
        }
        _ => Err(AppError::new(
            "invalid_config",
            "DSH 管理区块标记不完整，请先检查配置",
        )),
    }
}
