use crate::error::{AppError, Result};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Family {
    Codex,
    Claude,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Target {
    Codex,
    ClaudeDesktop,
    ClaudeCli,
}

impl Target {
    pub fn id(self) -> &'static str {
        match self {
            Self::Codex => "codex",
            Self::ClaudeDesktop => "claude_desktop",
            Self::ClaudeCli => "claude_cli",
        }
    }
    pub fn family(self) -> Family {
        match self {
            Self::Codex => Family::Codex,
            _ => Family::Claude,
        }
    }
}

/// Opaque approval for one exact provider, target and current file snapshot.
/// File contents and credentials are retained only by the backend.
#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ApplyOverwriteConfirmation {
    pub token: String,
    pub target: Target,
    pub provider_id: String,
    pub directory: String,
    pub files: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ProviderInput {
    pub id: Option<String>,
    pub family: Family,
    pub name: String,
    pub base_url: String,
    pub api_key: Option<String>,
    #[serde(default)]
    pub balance_access_token: Option<String>,
    pub model: String,
    pub auth_mode: String,
    pub reasoning_effort: Option<String>,
    #[serde(default)]
    pub codex_options: CodexOptions,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Provider {
    pub id: String,
    pub family: Family,
    pub name: String,
    pub base_url: String,
    pub model: String,
    pub auth_mode: String,
    pub reasoning_effort: Option<String>,
    #[serde(default)]
    pub codex_options: CodexOptions,
    pub has_key: bool,
    #[serde(default)]
    pub has_balance_token: bool,
    pub key_suffix: String,
    pub updated_at: u64,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct CodexOptions {
    /// Shared upstream connection; absent on records created before 0.4.
    pub upstream_protocol: Option<CodexProtocol>,
    pub protocol_preference: Option<CodexProtocol>,
    pub auth_preference: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub protocol_detected_at: Option<u64>,
    /// Per-client opt-out; older providers keep automatic conversion enabled.
    #[serde(skip_serializing_if = "Vec::is_empty")]
    pub conversion_disabled_targets: Vec<Target>,
    pub protocol: CodexProtocol,
    #[serde(skip_serializing_if = "native_claude_protocol")]
    pub claude_protocol: ClaudeProtocol,
    pub repair_reasoning_levels: bool,
    pub fast_mode: Option<bool>,
    pub context_window: Option<i64>,
    pub auto_compact_token_limit: Option<i64>,
    pub models: Vec<ProviderModel>,
    pub models_synced_at: Option<u64>,
    pub balance_query: Option<BalanceQuery>,
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum CodexProtocol {
    #[default]
    Openai,
    Anthropic,
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ClaudeProtocol {
    Openai,
    #[default]
    Anthropic,
}
fn native_claude_protocol(protocol: &ClaudeProtocol) -> bool {
    *protocol == ClaudeProtocol::Anthropic
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ProviderModel {
    pub id: String,
    #[serde(default)]
    pub context_window: Option<i64>,
    #[serde(default)]
    pub reasoning_efforts: Vec<String>,
    #[serde(default = "selected_by_default")]
    pub enabled: bool,
    #[serde(default, skip_serializing_if = "ModelCapabilities::is_empty")]
    pub capabilities: ModelCapabilities,
    #[serde(default, skip_serializing_if = "ModelCapabilities::is_empty")]
    pub capability_overrides: ModelCapabilities,
}
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ModelCapabilities {
    #[serde(default)]
    pub image_input: Option<bool>,
    #[serde(default)]
    pub parallel_tool_calls: Option<bool>,
}
impl ModelCapabilities {
    pub fn is_empty(&self) -> bool {
        self.image_input.is_none() && self.parallel_tool_calls.is_none()
    }
}

fn selected_by_default() -> bool {
    true
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum BalanceAdapter {
    Auto,
    #[default]
    Custom,
    Credit,
    Sub2api,
    NewapiAccount,
    NewapiToken,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct BalanceQuery {
    pub adapter: BalanceAdapter,
    pub site_url: Option<String>,
    pub user_id: Option<String>,
    pub path: String,
    pub json_path: String,
    pub unit: String,
    pub divisor: f64,
}
impl Default for BalanceQuery {
    fn default() -> Self {
        Self {
            adapter: BalanceAdapter::Custom,
            site_url: None,
            user_id: None,
            path: String::new(),
            json_path: String::new(),
            unit: "USD".into(),
            divisor: 1.0,
        }
    }
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ConnectionInput {
    pub provider_id: Option<String>,
    pub base_url: String,
    pub api_key: Option<String>,
    #[serde(default)]
    pub balance_access_token: Option<String>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ModelSyncResult {
    pub models: Vec<ProviderModel>,
    pub synced_at: u64,
    pub protocol: CodexProtocol,
    pub auth_mode: String,
    pub base_url: String,
}

impl Provider {
    pub fn needs_conversion(&self, target: Target) -> bool {
        (target == Target::Codex) != (self.upstream_protocol() == CodexProtocol::Openai)
    }
    pub fn conversion_enabled(&self, target: Target) -> bool {
        !self
            .codex_options
            .conversion_disabled_targets
            .contains(&target)
    }
    pub fn ensure_conversion(&self, target: Target) -> Result<()> {
        if self.needs_conversion(target) && !self.conversion_enabled(target) {
            return Err(AppError::new(
                "conversion_required",
                if target == Target::Codex {
                    "此供应商使用 Claude 协议，请先开启“转换为 OpenAI”再使用"
                } else {
                    "此供应商使用 OpenAI 协议，请先开启“转换为 Claude”再使用"
                },
            ));
        }
        Ok(())
    }
    pub fn upstream_protocol(&self) -> CodexProtocol {
        self.codex_options.upstream_protocol.unwrap_or_else(|| {
            if self.family == Family::Codex {
                self.codex_options.protocol
            } else if self.codex_options.claude_protocol == ClaudeProtocol::Openai {
                CodexProtocol::Openai
            } else {
                CodexProtocol::Anthropic
            }
        })
    }
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ModelWriteInput {
    pub connection: ConnectionInput,
    pub auth_mode: String,
    pub model: String,
    pub models: Vec<ProviderModel>,
    pub synced_at: u64,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ModelWriteResult {
    pub provider: Provider,
    pub applied: bool,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct QuickModelInput {
    pub expected: Provider,
    pub target: Target,
    pub model: String,
    pub models: Vec<ProviderModel>,
    pub synced_at: Option<u64>,
    pub repair_reasoning_levels: Option<bool>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ReasoningRepairResult {
    pub provider: Provider,
    pub applied: bool,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct FastModeResult {
    pub provider: Provider,
    pub applied: bool,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ProtocolConversionResult {
    pub provider: Provider,
    pub restored: bool,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct BalanceResult {
    pub amount: Option<f64>,
    pub unit: String,
    pub checked_at: u64,
    pub scope: String,
    pub unlimited: bool,
    pub used: Option<f64>,
    pub total: Option<f64>,
    pub plan_name: Option<String>,
    pub expires_at: Option<String>,
    pub windows: Vec<BalanceWindow>,
    pub provider_type: Option<String>,
    pub note: Option<String>,
}
#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct BalanceWindow {
    pub label: String,
    pub remaining: f64,
    pub used: f64,
    pub total: f64,
    pub reset_at: Option<String>,
}

pub fn valid_effort(effort: &str) -> bool {
    matches!(
        effort,
        "none" | "minimal" | "low" | "medium" | "high" | "xhigh" | "max" | "ultra"
    )
}

pub fn validate_url(value: &str) -> Result<url::Url> {
    let url = url::Url::parse(value)
        .map_err(|_| AppError::new("invalid_url", "API 地址必须是完整的 http 或 https URL"))?;
    if !matches!(url.scheme(), "http" | "https")
        || url.host_str().is_none()
        || !url.username().is_empty()
        || url.password().is_some()
        || url.query().is_some()
        || url.fragment().is_some()
    {
        return Err(AppError::new(
            "invalid_url",
            "API 地址不能包含账号、密码、查询参数或片段",
        ));
    }
    Ok(url)
}

pub fn validate_balance(query: &BalanceQuery) -> Result<()> {
    if let Some(site) = &query.site_url {
        validate_url(site)?;
    }
    if query.adapter == BalanceAdapter::NewapiAccount
        && !query.user_id.as_deref().is_some_and(|id| {
            !id.is_empty()
                && id.len() <= 20
                && id.bytes().all(|c| c.is_ascii_digit())
                && id.parse::<u64>().is_ok_and(|n| n > 0)
        })
    {
        return Err(AppError::new(
            "balance_user_id",
            "请填写 New API 控制台的数字用户 ID",
        ));
    }
    if query.adapter == BalanceAdapter::Custom
        && (!query.path.starts_with('/')
            || query.path.starts_with("//")
            || query.path.contains(['?', '#', '\\'])
            || query.path.chars().any(char::is_control)
            || query.path.len() > 512)
    {
        return Err(AppError::new(
            "invalid_balance_path",
            "余额接口请填写同一供应商的路径，以 / 开头，不含域名或查询参数",
        ));
    }
    if query.adapter == BalanceAdapter::Custom
        && (query.json_path.is_empty()
            || query.json_path.len() > 256
            || query
                .json_path
                .split('.')
                .any(|p| p.is_empty() || !p.chars().all(|c| c.is_ascii_alphanumeric() || c == '_')))
    {
        return Err(AppError::new(
            "invalid_balance_field",
            "余额字段请使用 data.balance 这样的 JSON 字段路径",
        ));
    }
    if !query.divisor.is_finite()
        || query.divisor <= 0.0
        || query.unit.chars().count() > 16
        || query.unit.chars().any(char::is_control)
    {
        return Err(AppError::new(
            "invalid_balance_unit",
            "余额换算除数必须大于 0，单位最多 16 个字符",
        ));
    }
    Ok(())
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StoredProvider {
    pub summary: Provider,
    pub api_key: String,
    #[serde(default)]
    pub balance_access_token: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct TargetStatus {
    pub target: Target,
    pub directory: String,
    pub files: Vec<String>,
    pub active_provider_id: Option<String>,
    pub state: String,
    pub can_restore: bool,
    pub message: String,
    #[serde(default)]
    pub applied_model: Option<String>,
    /// Read-back global config choices; not the model/effort of a live thread.
    pub configured_model: Option<String>,
    pub configured_reasoning_effort: Option<String>,
    pub configuration_revision: u64,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Overview {
    pub providers: Vec<Provider>,
    pub targets: Vec<TargetStatus>,
    pub data_directory: String,
    pub repaired_model_capabilities: bool,
}

pub fn validate(input: &mut ProviderInput) -> Result<()> {
    input.name = input.name.trim().to_owned();
    input.model = input.model.trim().to_owned();
    input.base_url = input.base_url.trim().trim_end_matches('/').to_owned();
    if input.name.is_empty() || input.name.len() > 160 {
        return Err(AppError::new(
            "invalid_name",
            "请填写 1–80 个字符的配置名称",
        ));
    }
    if input.model.is_empty()
        || input.model.len() > 256
        || input.model.chars().any(char::is_control)
    {
        return Err(AppError::new("invalid_model", "请选择有效的默认模型"));
    }
    validate_url(&input.base_url)?;
    if !matches!(input.auth_mode.as_str(), "bearer" | "x-api-key") {
        return Err(AppError::new(
            "invalid_auth",
            "请选择 Bearer Token 或 API Key 认证",
        ));
    }
    if input.codex_options.upstream_protocol.is_none()
        && input.family == Family::Codex
        && input.codex_options.protocol == CodexProtocol::Openai
    {
        input.auth_mode = "bearer".into();
    }
    if input.codex_options.upstream_protocol.is_none()
        && input.family == Family::Claude
        && input.codex_options.claude_protocol == ClaudeProtocol::Openai
    {
        input.auth_mode = "bearer".into();
    }
    if let Some(effort) = &input.reasoning_effort {
        if !valid_effort(effort) {
            return Err(AppError::new("invalid_effort", "推理强度无效"));
        }
    }
    let options = &mut input.codex_options;
    if options
        .auth_preference
        .as_deref()
        .is_some_and(|a| !matches!(a, "bearer" | "x-api-key"))
    {
        return Err(AppError::new("invalid_auth", "认证方式覆盖无效"));
    }
    if input.family == Family::Codex
        && options.protocol == CodexProtocol::Anthropic
        && options.fast_mode == Some(true)
    {
        return Err(AppError::new(
            "anthropic_fast",
            "Claude 协议转换暂不支持 priority/Fast 档位，请关闭 Fast 模式",
        ));
    }
    if input.family == Family::Codex
        && options.repair_reasoning_levels
        && !options.models.iter().any(|m| m.enabled)
    {
        options.models = vec![ProviderModel {
            id: input.model.clone(),
            context_window: None,
            reasoning_efforts: vec![],
            enabled: true,
            ..Default::default()
        }];
    }
    for size in [options.context_window, options.auto_compact_token_limit]
        .into_iter()
        .flatten()
    {
        if !(1..=100_000_000).contains(&size) {
            return Err(AppError::new(
                "invalid_context",
                "上下文和压缩阈值必须是 1–100000000 之间的整数 Token 数",
            ));
        }
    }
    if options
        .context_window
        .zip(options.auto_compact_token_limit)
        .is_some_and(|(window, limit)| limit > window)
    {
        return Err(AppError::new(
            "invalid_context",
            "自动压缩阈值不能超过上下文窗口",
        ));
    }
    if options.models.len() > 500 {
        return Err(AppError::new(
            "too_many_models",
            "最多保存 500 个模型，请缩小供应商的模型列表",
        ));
    }
    let mut ids = std::collections::HashSet::new();
    for model in &mut options.models {
        model.id = model.id.trim().to_owned();
        if model.id.is_empty()
            || model.id.len() > 256
            || model.id.chars().any(char::is_control)
            || !ids.insert(model.id.clone())
            || model.reasoning_efforts.iter().any(|e| !valid_effort(e))
            || model
                .context_window
                .is_some_and(|w| !(1..=100_000_000).contains(&w))
        {
            return Err(AppError::new(
                "invalid_models",
                "模型 ID 不能重复或包含控制字符，模型能力参数必须有效",
            ));
        }
    }
    if options.models.iter().any(|m| m.enabled)
        && !options
            .models
            .iter()
            .any(|m| m.enabled && m.id == input.model)
    {
        return Err(AppError::new(
            "default_model_missing",
            "默认模型必须包含在启用的模型列表中",
        ));
    }
    if let Some(query) = &options.balance_query {
        validate_balance(query)?;
    }
    if input
        .api_key
        .as_ref()
        .is_some_and(|key| key.chars().any(char::is_control))
    {
        return Err(AppError::new(
            "invalid_key",
            "API Key 不能包含换行或控制字符",
        ));
    }
    Ok(())
}
