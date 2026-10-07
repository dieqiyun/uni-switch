use crate::adapters::{self, Change, ManagedFile};
use crate::error::{AppError, Result};
use crate::types::*;
use crate::writer;
use rusqlite::{params, Connection, OptionalExtension};
use serde::{Deserialize, Serialize};
use std::path::PathBuf;
use std::time::{SystemTime, UNIX_EPOCH};

pub struct Store {
    conn: Connection,
    pub data_directory: PathBuf,
    _lock: std::fs::File,
    bridge_route: Option<crate::bridge::Route>,
}

#[derive(Debug, Serialize, Deserialize)]
struct Pending {
    target: Target,
    changes: Vec<Change>,
    baseline: Option<Vec<ManagedFile>>,
    active: Option<String>,
    #[serde(default)]
    provider_update: Option<StoredProvider>,
    #[serde(default)]
    applied_summary: Option<Provider>,
    #[serde(default)]
    accepted_source: Option<String>,
}

fn now() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs()
}
fn json<T: Serialize>(value: &T) -> Result<String> {
    serde_json::to_string(value).map_err(|_| AppError::new("serialization", "无法保存配置状态"))
}
fn decode<T: serde::de::DeserializeOwned>(value: &str) -> Result<T> {
    serde_json::from_str(value)
        .map_err(|_| AppError::new("invalid_state", "本地配置状态损坏，请保留数据目录并修复"))
}
fn decode_provider(data: &str) -> Result<StoredProvider> {
    let mut provider: StoredProvider = decode(data)?;
    let value: serde_json::Value = decode(data)?;
    if let Some(query) = &mut provider.summary.codex_options.balance_query {
        let legacy = value
            .pointer("/summary/codexOptions/balanceQuery/adapter")
            .is_none();
        if legacy
            && query.path.trim_end_matches('/') == "/api/usage/token"
            && query.json_path == "data.total_available"
            && query.unit == "额度"
            && query.divisor == 1.0
        {
            query.adapter = BalanceAdapter::NewapiToken;
            query.unit = "USD".into();
            query.divisor = 500_000.0;
        }
    }
    Ok(provider)
}
fn valid_private_token(token: &str) -> Result<()> {
    if token.len() > 8192 || token.chars().any(char::is_control) {
        return Err(AppError::new(
            "invalid_token",
            "Access Token 过长或包含换行、控制字符",
        ));
    }
    Ok(())
}

impl Store {
    pub fn open(directory: PathBuf) -> Result<Self> {
        writer::private_directory(&directory)?;
        let lock_path = directory.join("app.lock");
        let lock = std::fs::OpenOptions::new()
            .read(true)
            .write(true)
            .create(true)
            .truncate(false)
            .open(&lock_path)
            .map_err(|err| AppError::io(&lock_path, err))?;
        lock.try_lock().map_err(|_| {
            AppError::new("already_running", "uni-switch 已在运行，请使用已打开的窗口")
        })?;
        let conn = Connection::open(directory.join("uni-switch.db"))?;
        conn.execute_batch("PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL;
            CREATE TABLE IF NOT EXISTS providers (id TEXT PRIMARY KEY, family TEXT NOT NULL, data TEXT NOT NULL);
            CREATE TABLE IF NOT EXISTS targets (id TEXT PRIMARY KEY, directory TEXT NOT NULL, active TEXT, baseline TEXT);
            CREATE TABLE IF NOT EXISTS operations (id TEXT PRIMARY KEY, status TEXT NOT NULL, data TEXT NOT NULL, created_at INTEGER NOT NULL);
            PRAGMA user_version=1;")?;
        for target in [Target::Codex, Target::ClaudeDesktop, Target::ClaudeCli] {
            let default = adapters::default_directory(target)?;
            conn.execute(
                "INSERT OR IGNORE INTO targets(id,directory) VALUES(?1,?2)",
                params![target.id(), default.to_string_lossy()],
            )?;
        }
        let has_selection_column = conn
            .prepare("PRAGMA table_info(targets)")?
            .query_map([], |r| r.get::<_, String>(1))?
            .collect::<std::result::Result<Vec<_>, _>>()?
            .iter()
            .any(|name| name == "directory_selected");
        if !has_selection_column {
            conn.execute(
                "ALTER TABLE targets ADD COLUMN directory_selected INTEGER NOT NULL DEFAULT 0",
                [],
            )?;
            for target in [Target::Codex, Target::ClaudeDesktop, Target::ClaudeCli] {
                let saved: String = conn.query_row(
                    "SELECT directory FROM targets WHERE id=?1",
                    [target.id()],
                    |r| r.get(0),
                )?;
                if !crate::discovery::same_directory(
                    std::path::Path::new(&saved),
                    &adapters::default_directory(target)?,
                ) {
                    conn.execute(
                        "UPDATE targets SET directory_selected=1 WHERE id=?1",
                        [target.id()],
                    )?;
                }
            }
        }
        let mut store = Self {
            conn,
            data_directory: directory,
            _lock: lock,
            bridge_route: None,
        };
        let has_applied_at = store
            .conn
            .prepare("PRAGMA table_info(targets)")?
            .query_map([], |r| r.get::<_, String>(1))?
            .collect::<std::result::Result<Vec<_>, _>>()?
            .iter()
            .any(|n| n == "applied_at_ms");
        if !has_applied_at {
            store.conn.execute(
                "ALTER TABLE targets ADD COLUMN applied_at_ms INTEGER NOT NULL DEFAULT 0",
                [],
            )?;
        }
        let columns = store
            .conn
            .prepare("PRAGMA table_info(targets)")?
            .query_map([], |r| r.get::<_, String>(1))?
            .collect::<std::result::Result<Vec<_>, _>>()?;
        for column in ["applied_summary", "accepted_source"] {
            if !columns.iter().any(|c| c == column) {
                store
                    .conn
                    .execute(&format!("ALTER TABLE targets ADD COLUMN {column} TEXT"), [])?;
            }
        }
        store.recover()?;
        Ok(store)
    }

    pub fn set_bridge_route(&mut self, route: crate::bridge::Route) {
        self.bridge_route = Some(route);
    }
    pub fn runtime_context(
        &self,
        target: Target,
    ) -> Result<(PathBuf, u64, bool, Option<crate::bridge::Route>)> {
        let (directory, _, _) = self.target_record(target)?;
        let applied_at = self.conn.query_row(
            "SELECT applied_at_ms FROM targets WHERE id=?1",
            [target.id()],
            |r| r.get(0),
        )?;
        Ok((
            directory,
            applied_at,
            self.bridge_record(target)?.is_some(),
            self.bridge_route.clone(),
        ))
    }
    pub fn provider_requires_bridge(&self, target: Target, id: &str) -> Result<bool> {
        let provider = self.provider(id)?.summary;
        provider.ensure_conversion(target)?;
        Ok(provider.needs_conversion(target))
    }

    pub fn bridge_required(&self) -> bool {
        [Target::Codex, Target::ClaudeDesktop, Target::ClaudeCli]
            .into_iter()
            .any(|target| self.bridge_record(target).is_ok_and(|p| p.is_some()))
    }

    fn bridge_provider_record(&self) -> Result<Option<StoredProvider>> {
        self.bridge_record(Target::Codex)
    }

    fn bridge_path(&self, target: Target) -> PathBuf {
        self.data_directory.join(match target {
            Target::Codex => "bridge-active.json",
            Target::ClaudeDesktop => "bridge-claude-desktop-active.json",
            Target::ClaudeCli => "bridge-claude-cli-active.json",
        })
    }

    fn bridge_record(&self, target: Target) -> Result<Option<StoredProvider>> {
        adapters::read(&self.bridge_path(target))?
            .map(|s| decode(&s))
            .transpose()
    }

    pub fn claude_bridge_provider(&self, target: Target, id: &str) -> Result<StoredProvider> {
        let (_, active, _) = self.target_record(target)?;
        self.bridge_record(target)?
            .filter(|p| {
                target.family() == Family::Claude
                    && active.as_deref() == Some(id)
                    && p.summary.id == id
                    && p.summary.codex_options.claude_protocol == ClaudeProtocol::Openai
            })
            .ok_or_else(|| {
                AppError::new(
                    "bridge_inactive",
                    "此 GPT 转换配置尚未应用，或已切换供应商，请重新应用",
                )
            })
    }

    pub fn bridge_provider(&self, id: &str) -> Result<StoredProvider> {
        let (_, active, _) = self.target_record(Target::Codex)?;
        let saved = self
            .bridge_provider_record()?
            .filter(|p| {
                active.as_deref() == Some(id)
                    && p.summary.id == id
                    && p.summary.codex_options.protocol == CodexProtocol::Anthropic
            })
            .ok_or_else(|| {
                AppError::new(
                    "bridge_inactive",
                    "此 Claude 转换配置尚未应用，或已切换供应商，请重新应用",
                )
            })?;
        Ok(saved)
    }

    fn plan(
        &self,
        target: Target,
        directory: &std::path::Path,
        provider: &StoredProvider,
    ) -> Result<Vec<ManagedFile>> {
        provider.summary.ensure_conversion(target)?;
        let projected = Self::for_target(provider, target);
        let provider = &projected;
        let mut routed = provider.clone();
        let converted = if target == Target::Codex {
            provider.summary.codex_options.protocol == CodexProtocol::Anthropic
        } else {
            provider.summary.codex_options.claude_protocol == ClaudeProtocol::Openai
        };
        if converted {
            let route = self.bridge_route.as_ref().ok_or_else(|| {
                AppError::new(
                    "bridge_unavailable",
                    "本地协议转换服务尚未启动，请重启 uni-switch",
                )
            })?;
            routed.summary.base_url = if target == Target::Codex {
                route.base_url(&provider.summary.id)
            } else {
                route.claude_base_url(target, &provider.summary.id)
            };
            routed.api_key = route.token.clone();
            routed.summary.auth_mode = "bearer".into();
            if target.family() == Family::Claude {
                routed.summary.auth_mode = "bearer".into();
                if target == Target::ClaudeDesktop {
                    routed.summary.model =
                        crate::bridge::reverse::model_alias(&provider.summary.model);
                    for model in &mut routed.summary.codex_options.models {
                        model.id = crate::bridge::reverse::model_alias(&model.id);
                    }
                }
            }
        }
        let mut files = adapters::plan(target, directory, &routed)?;
        if target == Target::ClaudeDesktop && converted {
            let ids = crate::bridge::reverse::model_ids(provider);
            if ids.len() > 200 {
                return Err(AppError::new(
                    "desktop_model_limit",
                    "Claude 桌面端最多启用 200 个模型，请减少勾选数量",
                ));
            }
            for file in &mut files {
                if file.keys.iter().any(|key| key == "inferenceModels") {
                    let mut doc: serde_json::Value =
                        serde_json::from_str(file.expected.as_deref().unwrap_or("{}"))
                            .map_err(|_| AppError::new("invalid_state", "Claude 桌面配置无效"))?;
                    doc["inferenceModels"] = serde_json::json!(ids
                        .iter()
                        .map(|id| serde_json::json!({
                            "name":crate::bridge::reverse::model_alias(id),"labelOverride":id
                        }))
                        .collect::<Vec<_>>());
                    file.expected = Some(serde_json::to_string_pretty(&doc).unwrap());
                }
            }
        }
        if target == Target::Codex && !converted && self.bridge_provider_record()?.is_some() {
            let (_, _, baseline) = self.target_record(target)?;
            if let Some(before) = baseline
                .as_ref()
                .and_then(|files| files.iter().find(|f| f.format == "toml"))
            {
                let original = before
                    .original
                    .as_deref()
                    .unwrap_or("")
                    .parse::<toml_edit::DocumentMut>()
                    .map_err(|_| AppError::new("invalid_state", "Codex 原配置快照无效"))?;
                if let Some(file) = files.iter_mut().find(|f| f.format == "toml") {
                    let mut doc = file
                        .expected
                        .as_deref()
                        .unwrap_or("")
                        .parse::<toml_edit::DocumentMut>()
                        .map_err(|_| AppError::new("invalid_state", "Codex 配置无效"))?;
                    if let Some(value) = original.get("web_search") {
                        doc["web_search"] = value.clone();
                    } else {
                        doc.as_table_mut().remove("web_search");
                    }
                    file.expected = Some(doc.to_string());
                }
            }
        }
        if target == Target::ClaudeCli && !converted && self.bridge_record(target)?.is_some() {
            let (_, _, baseline) = self.target_record(target)?;
            if let Some(before) = baseline.as_ref().and_then(|files| {
                files
                    .iter()
                    .find(|f| f.path == directory.join("settings.json"))
            }) {
                let original: serde_json::Value =
                    serde_json::from_str(before.original.as_deref().unwrap_or("{}"))
                        .map_err(|_| AppError::new("invalid_state", "Claude 原配置快照无效"))?;
                if let Some(file) = files.iter_mut().find(|f| f.path == before.path) {
                    let mut doc: serde_json::Value =
                        serde_json::from_str(file.expected.as_deref().unwrap_or("{}"))
                            .map_err(|_| AppError::new("invalid_state", "Claude 配置无效"))?;
                    if let Some(value) = original.pointer("/env/ENABLE_TOOL_SEARCH") {
                        doc["env"]["ENABLE_TOOL_SEARCH"] = value.clone();
                    } else if let Some(env) = doc["env"].as_object_mut() {
                        env.remove("ENABLE_TOOL_SEARCH");
                    }
                    file.keys.push("env.ENABLE_TOOL_SEARCH".into());
                    file.expected = Some(serde_json::to_string_pretty(&doc).unwrap());
                }
            }
        }
        {
            let path = self.bridge_path(target);
            files.push(ManagedFile {
                original: adapters::read(&path)?,
                path,
                format: "json".into(),
                keys: vec![
                    "summary".into(),
                    "api_key".into(),
                    "balance_access_token".into(),
                ],
                expected: if converted {
                    Some(json(provider)?)
                } else {
                    None
                },
            });
        }
        Ok(files)
    }

    pub fn list(&self) -> Result<Vec<Provider>> {
        let mut stmt = self
            .conn
            .prepare("SELECT data FROM providers ORDER BY rowid DESC")?;
        let rows = stmt.query_map([], |row| row.get::<_, String>(0))?;
        rows.map(|row| decode_provider(&row?).map(|p| p.summary))
            .collect()
    }

    fn provider(&self, id: &str) -> Result<StoredProvider> {
        let data: Option<String> = self
            .conn
            .query_row("SELECT data FROM providers WHERE id=?1", [id], |row| {
                row.get(0)
            })
            .optional()?;
        decode_provider(
            &data.ok_or_else(|| AppError::new("not_found", "配置已不存在，请刷新列表"))?,
        )
    }

    fn account_token(
        &self,
        base_url: &str,
        query: &BalanceQuery,
        token: Option<String>,
        old: Option<&StoredProvider>,
    ) -> Result<String> {
        let token = token
            .filter(|v| !v.trim().is_empty())
            .or_else(|| {
                old.filter(|p| {
                    p.summary
                        .codex_options
                        .balance_query
                        .as_ref()
                        .is_some_and(|saved| {
                            saved.adapter == BalanceAdapter::NewapiAccount
                                && crate::supplier::balance_site_url(&p.summary.base_url, saved)
                                    .ok()
                                    == crate::supplier::balance_site_url(base_url, query).ok()
                        })
                })
                .and_then(|p| p.balance_access_token.clone())
            })
            .ok_or_else(|| {
                AppError::new(
                    "missing_balance_token",
                    "请填写 New API 控制台 Access Token；更换查询站点后需要重新填写",
                )
            })?;
        valid_private_token(&token)?;
        Ok(token.trim().to_owned())
    }

    pub fn balance_connection(
        &self,
        input: ConnectionInput,
        query: &BalanceQuery,
    ) -> Result<(String, String)> {
        validate_balance(query)?;
        validate_url(input.base_url.trim())?;
        if query.adapter != BalanceAdapter::NewapiAccount {
            return self.connection(input);
        }
        let old = input
            .provider_id
            .as_deref()
            .map(|id| self.provider(id))
            .transpose()?;
        let token = self.account_token(
            &input.base_url,
            query,
            input.balance_access_token,
            old.as_ref(),
        )?;
        Ok((
            input.base_url.trim().trim_end_matches('/').to_owned(),
            token,
        ))
    }

    pub fn auto_balance_connection(
        &self,
        input: ConnectionInput,
        query: Option<BalanceQuery>,
    ) -> Result<(String, String, Option<BalanceQuery>, Option<String>)> {
        let old = input
            .provider_id
            .as_deref()
            .map(|id| self.provider(id))
            .transpose()?;
        let query = query.or_else(|| {
            old.as_ref()
                .and_then(|p| p.summary.codex_options.balance_query.clone())
        });
        if let Some(query) = &query {
            validate_balance(query)?;
        }
        let token = query
            .as_ref()
            .filter(|q| q.adapter == BalanceAdapter::NewapiAccount)
            .and_then(|q| {
                self.account_token(
                    &input.base_url,
                    q,
                    input.balance_access_token.clone(),
                    old.as_ref(),
                )
                .ok()
            });
        let (base_url, key) = self.connection(input)?;
        Ok((base_url, key, query, token))
    }

    pub fn connection(&self, input: ConnectionInput) -> Result<(String, String)> {
        validate_url(input.base_url.trim())?;
        let old = input
            .provider_id
            .as_deref()
            .map(|id| self.provider(id))
            .transpose()?;
        let key = input
            .api_key
            .filter(|k| !k.trim().is_empty())
            .or_else(|| old.map(|p| p.api_key))
            .ok_or_else(|| AppError::new("missing_key", "请填写 API Key 后再查询"))?;
        if key.chars().any(char::is_control) {
            return Err(AppError::new("invalid_key", "API Key 不能包含控制字符"));
        }
        Ok((
            input.base_url.trim().trim_end_matches('/').to_owned(),
            key.trim().to_owned(),
        ))
    }

    fn prepare_provider(&self, mut input: ProviderInput) -> Result<StoredProvider> {
        let old = input
            .id
            .as_deref()
            .map(|id| self.provider(id))
            .transpose()?;
        // A stale open form or a subsequent model refresh must not undo a repair.
        input.codex_options.repair_reasoning_levels |= old
            .as_ref()
            .is_some_and(|p| p.summary.codex_options.repair_reasoning_levels);
        validate(&mut input)?;
        let key = input
            .api_key
            .filter(|key| !key.trim().is_empty())
            .map(|key| key.trim().to_owned())
            .or_else(|| old.as_ref().map(|p| p.api_key.clone()))
            .ok_or_else(|| AppError::new("missing_key", "请填写 API Key"))?;
        let balance_token = if let Some(query) = input
            .codex_options
            .balance_query
            .as_ref()
            .filter(|q| q.adapter == BalanceAdapter::NewapiAccount)
        {
            Some(self.account_token(
                &input.base_url,
                query,
                input.balance_access_token,
                old.as_ref(),
            )?)
        } else {
            None
        };
        let suffix = key
            .chars()
            .rev()
            .take(4)
            .collect::<Vec<_>>()
            .into_iter()
            .rev()
            .collect();
        let provider = Provider {
            id: input.id.unwrap_or_else(|| uuid::Uuid::new_v4().to_string()),
            family: input.family,
            name: input.name,
            base_url: input.base_url,
            model: input.model,
            auth_mode: input.auth_mode,
            reasoning_effort: input.reasoning_effort,
            codex_options: input.codex_options,
            has_key: true,
            has_balance_token: balance_token.is_some(),
            key_suffix: suffix,
            updated_at: now(),
        };
        let stored = StoredProvider {
            summary: provider.clone(),
            api_key: key,
            balance_access_token: balance_token,
        };
        Ok(stored)
    }

    pub fn save(&mut self, input: ProviderInput) -> Result<Provider> {
        let stored = self.prepare_provider(input)?;
        let provider = stored.summary.clone();
        self.conn.execute("INSERT INTO providers(id,family,data) VALUES(?1,?2,?3) ON CONFLICT(id) DO UPDATE SET family=excluded.family,data=excluded.data", params![provider.id, json(&provider.family)?, json(&stored)?])?;
        Ok(provider)
    }

    /// The provider and client files commit through the same recoverable journal.
    /// Failure before/during application leaves the previous provider intact.
    pub fn commit_provider(
        &mut self,
        input: ProviderInput,
        target: Target,
        apply: bool,
    ) -> Result<Provider> {
        self.recover()?;
        if !apply {
            return self.save(input);
        }
        let stored = self.prepare_provider(input)?;
        let summary = stored.summary.clone();
        self.apply_stored(target, stored.clone(), Some(stored), false)?;
        Ok(summary)
    }

    pub fn commit_unique_provider(
        &mut self,
        input: ProviderInput,
        target: Target,
        apply: bool,
    ) -> Result<(Provider, bool)> {
        if input.id.is_none() {
            let prepared = self.prepare_provider(input.clone())?;
            for existing in self.list()? {
                let stored = self.provider(&existing.id)?;
                if stored.summary.base_url == prepared.summary.base_url
                    && stored.api_key == prepared.api_key
                    && stored.summary.auth_mode == prepared.summary.auth_mode
                    && stored.summary.upstream_protocol() == prepared.summary.upstream_protocol()
                    && stored.summary.model == prepared.summary.model
                    && stored.summary.reasoning_effort == prepared.summary.reasoning_effort
                    && Self::model_settings_signature(&stored.summary)?
                        == Self::model_settings_signature(&prepared.summary)?
                {
                    if apply {
                        self.apply(target, &existing.id)?;
                    }
                    return Ok((existing, true));
                }
            }
        }
        self.commit_provider(input, target, apply)
            .map(|p| (p, false))
    }

    fn for_target(provider: &StoredProvider, target: Target) -> StoredProvider {
        let mut projected = provider.clone();
        let upstream = provider.summary.upstream_protocol();
        projected.summary.family = target.family();
        projected.summary.codex_options.protocol = upstream;
        projected.summary.codex_options.claude_protocol = if upstream == CodexProtocol::Openai {
            ClaudeProtocol::Openai
        } else {
            ClaudeProtocol::Anthropic
        };
        if upstream == CodexProtocol::Anthropic {
            projected.summary.codex_options.fast_mode = None;
        }
        if target == Target::Codex
            && projected.summary.codex_options.models.is_empty()
            && provider.summary.family == Family::Claude
        {
            projected.summary.codex_options.models.push(ProviderModel {
                id: provider.summary.model.clone(),
                context_window: Some(256_000),
                reasoning_efforts: vec![],
                enabled: true,
            });
        }
        projected
    }
    fn model_settings_signature(provider: &Provider) -> Result<String> {
        let mut options = provider.codex_options.clone();
        options.models_synced_at = None;
        options.upstream_protocol = Some(provider.upstream_protocol());
        options.protocol_preference = None;
        options.auth_preference = None;
        options.protocol_detected_at = None;
        for model in &mut options.models {
            if model.context_window.is_none() {
                model.context_window = Some(256_000);
            }
        }
        json(&options)
    }
    fn source_signature(provider: &StoredProvider) -> Result<String> {
        use sha2::{Digest, Sha256};
        let mut value = serde_json::to_value(provider)
            .map_err(|_| AppError::new("serialization", "无法比较供应商配置"))?;
        if let Some(summary) = value
            .get_mut("summary")
            .and_then(serde_json::Value::as_object_mut)
        {
            for field in ["name", "updatedAt", "modelsSyncedAt"] {
                summary.remove(field);
            }
            if let Some(options) = summary
                .get_mut("codexOptions")
                .and_then(serde_json::Value::as_object_mut)
            {
                options.remove("modelsSyncedAt");
                options.remove("protocolDetectedAt");
                options.remove("conversionDisabledTargets");
            }
        }
        Ok(format!("{:x}", Sha256::digest(json(&value)?.as_bytes())))
    }
    pub fn sync_connection(&mut self, target: Target, provider_id: &str) -> Result<TargetStatus> {
        self.recover()?;
        let newest = self.provider(provider_id)?;
        let (directory, active, baseline) = self.target_record(target)?;
        if active.as_deref() != Some(provider_id) {
            return Err(AppError::new(
                "target_changed",
                "此客户端已切换供应商，未同步",
            ));
        }
        let signature = Self::source_signature(&newest)?;
        let saved: Option<String> = self.conn.query_row(
            "SELECT applied_summary FROM targets WHERE id=?1",
            [target.id()],
            |r| r.get(0),
        )?;
        let old = saved
            .map(|text| decode::<Provider>(&text))
            .transpose()?
            .or_else(|| self.bridge_record(target).ok().flatten().map(|p| p.summary))
            .or_else(|| {
                adapters::import(target, &directory).ok().map(|imported| {
                    let mut summary = newest.summary.clone();
                    summary.model = imported.model;
                    summary.reasoning_effort = imported.reasoning_effort;
                    summary.codex_options.fast_mode = imported.codex_options.fast_mode;
                    summary.codex_options.context_window = imported.codex_options.context_window;
                    summary.codex_options.auto_compact_token_limit =
                        imported.codex_options.auto_compact_token_limit;
                    summary.codex_options.models = imported.codex_options.models;
                    summary.codex_options.repair_reasoning_levels =
                        baseline.as_ref().is_some_and(|files| {
                            files.iter().any(|f| {
                                f.keys
                                    .iter()
                                    .any(|key| key == adapters::REASONING_DISPLAY_KEY)
                            })
                        });
                    summary
                })
            });
        let mut merged = newest.clone();
        if let Some(old) = old {
            merged.summary.model = old.model;
            merged.summary.reasoning_effort = old.reasoning_effort;
            merged.summary.codex_options = old.codex_options;
            merged.summary.codex_options.upstream_protocol =
                Some(newest.summary.upstream_protocol());
            merged.summary.codex_options.conversion_disabled_targets = newest
                .summary
                .codex_options
                .conversion_disabled_targets
                .clone();
        }
        self.apply_stored_from_source(target, merged, None, true, Some(signature))
    }

    pub fn delete(&mut self, id: &str) -> Result<()> {
        let in_use: bool = self.conn.query_row(
            "SELECT EXISTS(SELECT 1 FROM targets WHERE active=?1)",
            [id],
            |row| row.get(0),
        )?;
        if in_use {
            return Err(AppError::new(
                "in_use",
                "此配置仍在客户端中使用，请先应用其他配置或恢复原配置",
            ));
        }
        self.conn
            .execute("DELETE FROM providers WHERE id=?1", [id])?;
        Ok(())
    }

    fn target_record(
        &self,
        target: Target,
    ) -> Result<(PathBuf, Option<String>, Option<Vec<ManagedFile>>)> {
        let (directory, active, baseline): (String, Option<String>, Option<String>) =
            self.conn.query_row(
                "SELECT directory,active,baseline FROM targets WHERE id=?1",
                [target.id()],
                |row| Ok((row.get(0)?, row.get(1)?, row.get(2)?)),
            )?;
        Ok((
            directory.into(),
            active,
            baseline.map(|value| decode(&value)).transpose()?,
        ))
    }

    pub fn status(&self, target: Target) -> Result<TargetStatus> {
        let (directory, active, baseline) = self.target_record(target)?;
        let configuration_revision = self.conn.query_row(
            "SELECT applied_at_ms FROM targets WHERE id=?1",
            [target.id()],
            |r| r.get(0),
        )?;
        let applied_summary: Option<String> = self.conn.query_row(
            "SELECT applied_summary FROM targets WHERE id=?1",
            [target.id()],
            |r| r.get(0),
        )?;
        let applied_model = applied_summary
            .map(|text| decode::<Provider>(&text).map(|p| p.model))
            .transpose()?;
        let mut state = if active.is_some() {
            "applied"
        } else {
            "unmanaged"
        }
        .to_owned();
        let mut message = if active.is_some() {
            "配置已写入。请完全退出客户端，再重新打开以使用新配置。"
        } else {
            "添加或导入一组 API 配置，然后应用到此客户端。"
        }
        .to_owned();
        if let Some(files) = &baseline {
            let active_provider = active
                .as_deref()
                .map(|id| self.provider(id))
                .transpose()?
                .map(|p| Self::for_target(&p, target));
            for file in files {
                match adapters::changed_for_provider(file, active_provider.as_ref()) {
                    Ok(true) => {
                        state = "external_change".into();
                        message =
                            "API 字段已被其他工具修改。可以导入现有配置，或重新应用已保存的配置。"
                                .into();
                        break;
                    }
                    Err(err) => {
                        state = "error".into();
                        message = err.message;
                        break;
                    }
                    _ => {}
                }
            }
            if state == "applied" {
                if let Some(id) = &active {
                    if let Ok(provider) = self.provider(id) {
                        let accepted: Option<String> = self.conn.query_row(
                            "SELECT accepted_source FROM targets WHERE id=?1",
                            [target.id()],
                            |r| r.get(0),
                        )?;
                        match if accepted.as_deref() == Some(&Self::source_signature(&provider)?) {
                            Ok(Vec::new())
                        } else {
                            self.plan(target, &directory, &provider)
                        } {
                            Ok(desired) => {
                                for file in desired {
                                    let applied_file =
                                        files.iter().find(|old| old.path == file.path);
                                    let applied_content =
                                        applied_file.and_then(|old| old.expected.as_deref());
                                    if adapters::projection(&file, applied_content)?
                                        != adapters::projection(&file, file.expected.as_deref())?
                                    {
                                        state = "saved_changes".into();
                                        message = "此配置已编辑。点击重新应用，让客户端使用保存后的内容。".into();
                                        break;
                                    }
                                }
                            }
                            Err(err) => {
                                state = "error".into();
                                message = err.message;
                            }
                        }
                    }
                }
            }
        }
        Ok(TargetStatus {
            target,
            directory: directory.to_string_lossy().into_owned(),
            files: adapters::specifications(target, &directory)
                .into_iter()
                .map(|(path, _, _)| path.to_string_lossy().into_owned())
                .collect(),
            active_provider_id: active,
            state,
            can_restore: baseline.is_some(),
            message,
            applied_model,
            configuration_revision,
        })
    }

    pub fn overview(&self) -> Result<Overview> {
        Ok(Overview {
            providers: self.list()?,
            targets: [Target::Codex, Target::ClaudeDesktop, Target::ClaudeCli]
                .into_iter()
                .map(|t| self.status(t))
                .collect::<Result<_>>()?,
            data_directory: self.data_directory.to_string_lossy().into_owned(),
        })
    }

    pub fn set_directory(&mut self, target: Target, directory: String) -> Result<()> {
        let path = PathBuf::from(directory.trim());
        if !path.is_absolute() {
            return Err(AppError::new("invalid_path", "请选择完整的绝对目录路径"));
        }
        let (current, _, baseline) = self.target_record(target)?;
        if crate::discovery::same_directory(&path, &current) {
            self.conn.execute(
                "UPDATE targets SET directory_selected=1 WHERE id=?1",
                [target.id()],
            )?;
            return Ok(());
        }
        if baseline.is_some() {
            return Err(AppError::new(
                "managed_directory",
                "请先恢复当前目录的原配置，再修改目录",
            ));
        }
        if path.is_file() {
            return Err(AppError::new(
                "invalid_path",
                "请输入目录路径，不是文件路径",
            ));
        }
        self.conn.execute(
            "UPDATE targets SET directory=?1,active=NULL,directory_selected=1,applied_at_ms=0,applied_summary=NULL,accepted_source=NULL WHERE id=?2",
            params![path.to_string_lossy(), target.id()],
        )?;
        Ok(())
    }
    pub fn directory_discovery_context(&self, target: Target) -> Result<(PathBuf, bool)> {
        let (directory, _, baseline) = self.target_record(target)?;
        let manual: bool = self.conn.query_row(
            "SELECT directory_selected FROM targets WHERE id=?1",
            [target.id()],
            |r| r.get(0),
        )?;
        Ok((directory, manual || baseline.is_some()))
    }
    pub fn select_detected_directory(
        &mut self,
        target: Target,
        directory: String,
        expected: &str,
        automatic: bool,
    ) -> Result<()> {
        let (current, selected) = self.directory_discovery_context(target)?;
        if !crate::discovery::same_directory(&current, std::path::Path::new(expected)) {
            return Err(AppError::new(
                "directory_changed",
                "当前目录已变化，请重新查找",
            ));
        }
        if automatic && selected {
            return Err(AppError::new(
                "directory_selected",
                "当前目录已手动选择或接管，不自动更换",
            ));
        }
        self.set_directory(target, directory)?;
        if automatic {
            self.conn.execute(
                "UPDATE targets SET directory_selected=0 WHERE id=?1",
                [target.id()],
            )?;
        }
        Ok(())
    }

    pub fn import(&mut self, target: Target) -> Result<Provider> {
        let (directory, _, baseline) = self.target_record(target)?;
        let input = adapters::import(target, &directory)?;
        {
            if let Some(provider) = self.bridge_record(target)? {
                if self.bridge_route.as_ref().is_some_and(|route| {
                    input.base_url
                        == if target == Target::Codex {
                            route.base_url(&provider.summary.id)
                        } else {
                            route.claude_base_url(target, &provider.summary.id)
                        }
                }) {
                    return Ok(provider.summary);
                }
            }
        }
        let provider = self.save(input)?;
        if let Some(mut baseline) = baseline {
            for file in &mut baseline {
                let current = adapters::read(&file.path)?;
                adapters::projection(file, current.as_deref())?;
                file.expected = current;
            }
            self.conn.execute(
                "UPDATE targets SET baseline=?1 WHERE id=?2",
                params![json(&baseline)?, target.id()],
            )?;
        }
        Ok(provider)
    }

    pub fn apply(&mut self, target: Target, provider_id: &str) -> Result<TargetStatus> {
        self.recover()?;
        let provider = self.provider(provider_id)?;
        self.apply_stored(target, provider, None, false)
    }

    fn apply_stored(
        &mut self,
        target: Target,
        provider: StoredProvider,
        provider_update: Option<StoredProvider>,
        preserve_choices: bool,
    ) -> Result<TargetStatus> {
        self.apply_stored_from_source(target, provider, provider_update, preserve_choices, None)
    }
    fn apply_stored_from_source(
        &mut self,
        target: Target,
        provider: StoredProvider,
        provider_update: Option<StoredProvider>,
        preserve_choices: bool,
        source: Option<String>,
    ) -> Result<TargetStatus> {
        let accepted_source = Some(source.unwrap_or(Self::source_signature(&provider)?));
        let provider = Self::for_target(&provider, target);
        let provider_id = provider.summary.id.clone();
        let (directory, active, baseline) = self.target_record(target)?;
        let active_provider = active
            .as_deref()
            .map(|id| self.provider(id))
            .transpose()?
            .map(|p| Self::for_target(&p, target));
        let mut files = self.plan(target, &directory, &provider)?;
        if let Some(baseline) = &baseline {
            for file in &mut files {
                if let Some(previous) = baseline.iter().find(|old| old.path == file.path) {
                    if previous.keys.iter().any(|k| k == "web_search")
                        && !file.keys.iter().any(|k| k == "web_search")
                    {
                        file.keys.push("web_search".into());
                    }
                    if previous
                        .keys
                        .iter()
                        .any(|key| key == adapters::REASONING_DISPLAY_KEY)
                        && !file
                            .keys
                            .iter()
                            .any(|key| key == adapters::REASONING_DISPLAY_KEY)
                    {
                        file.keys.push(adapters::REASONING_DISPLAY_KEY.into());
                    }
                    if adapters::changed_for_provider(previous, active_provider.as_ref())? {
                        return Err(AppError::new("external_change", "当前 API 字段已被其他工具修改。请先导入现有配置，然后恢复或重新确认目标配置；为保护现有文件，此次没有写入"));
                    }
                }
            }
        }
        let keep_model = active_provider
            .as_ref()
            .is_some_and(|p| p.summary.model == provider.summary.model);
        let changes: Vec<_> = files
            .iter()
            .map(|file| {
                Ok(Change {
                    path: file.path.clone(),
                    before: file.original.clone(),
                    after: if preserve_choices && target == Target::Codex && file.format == "toml" {
                        adapters::preserve_codex_choices(file, &provider, keep_model)?
                    } else {
                        file.expected.clone()
                    },
                })
            })
            .collect::<Result<Vec<_>>>()?
            .into_iter()
            .filter(|change| change.before != change.after)
            .collect();
        if let Some(baseline) = baseline {
            for file in &mut files {
                if let Some(previous) = baseline.iter().find(|old| old.path == file.path) {
                    // Keep the first snapshot of previously managed keys, while capturing
                    // newly introduced settings at the time we first take ownership of them.
                    let snapshot = ManagedFile {
                        expected: file.original.clone(),
                        original: previous.original.clone(),
                        keys: previous.keys.clone(),
                        ..file.clone()
                    };
                    file.original = adapters::restore(&snapshot)?.after;
                }
            }
        }
        self.execute(Pending {
            target,
            changes,
            baseline: Some(files),
            active: Some(provider_id),
            provider_update,
            applied_summary: Some(provider.summary.clone()),
            accepted_source,
        })?;
        self.status(target)
    }

    pub fn set_provider_fast_mode(
        &mut self,
        provider_id: &str,
        enabled: bool,
    ) -> Result<FastModeResult> {
        self.recover()?;
        let mut provider = self.provider(provider_id)?;
        if enabled && provider.summary.upstream_protocol() == CodexProtocol::Anthropic {
            return Err(AppError::new(
                "anthropic_fast",
                "Claude 协议转换暂不支持 Fast，请使用 OpenAI 协议供应商",
            ));
        }
        let was_applied = self.status(Target::Codex)?.state == "applied";
        let (directory, active, baseline) = self.target_record(Target::Codex)?;
        let applied = active.as_deref() == Some(provider_id);
        let previous_source = Self::source_signature(&provider)?;
        provider.summary.codex_options.fast_mode = Some(enabled);
        provider.summary.updated_at = now();
        if applied {
            let mut baseline = baseline.ok_or_else(|| {
                AppError::new(
                    "invalid_state",
                    "当前配置缺少管理记录，请重新使用供应商后重试",
                )
            })?;
            let previous = self.provider(provider_id)?;
            let summary: Option<String> = self.conn.query_row(
                "SELECT applied_summary FROM targets WHERE id=?1",
                [Target::Codex.id()],
                |r| r.get(0),
            )?;
            let mut applied_summary = summary
                .map(|text| decode::<Provider>(&text))
                .transpose()?
                .unwrap_or_else(|| previous.summary.clone());
            if enabled && applied_summary.upstream_protocol() == CodexProtocol::Anthropic {
                return Err(AppError::new(
                    "anthropic_fast",
                    "当前 Codex 仍在使用 Claude 协议转换，请先使用 OpenAI 协议配置再开启 Fast",
                ));
            }
            applied_summary.codex_options.fast_mode = Some(enabled);
            applied_summary.updated_at = provider.summary.updated_at;
            let applied_provider = StoredProvider {
                summary: applied_summary.clone(),
                ..previous.clone()
            };
            // Validate profile overrides and file shapes without applying pending edits.
            adapters::plan(
                Target::Codex,
                &directory,
                &Self::for_target(&applied_provider, Target::Codex),
            )?;
            let previous_for_codex = Self::for_target(&previous, Target::Codex);
            for file in &baseline {
                if adapters::changed_for_provider(file, Some(&previous_for_codex))? {
                    return Err(AppError::new("external_change", "当前 API 配置或模型目录已被其他工具修改，请先处理配置冲突后重试；此次没有写入"));
                }
            }
            let files = adapters::fast_mode_files(
                &baseline,
                &Self::for_target(&applied_provider, Target::Codex),
                enabled,
            )?;
            let changes = files
                .iter()
                .filter(|f| f.original != f.expected)
                .map(|f| Change {
                    path: f.path.clone(),
                    before: f.original.clone(),
                    after: f.expected.clone(),
                })
                .collect();
            for file in files {
                if let Some(previous) = baseline.iter_mut().find(|f| f.path == file.path) {
                    let snapshot = ManagedFile {
                        expected: file.original.clone(),
                        original: previous.original.clone(),
                        keys: previous.keys.clone(),
                        ..file.clone()
                    };
                    previous.original = adapters::restore(&snapshot)?.after;
                    previous.keys = file.keys;
                    previous.expected = file.expected;
                } else {
                    baseline.push(file);
                }
            }
            let accepted_source = if was_applied {
                Some(Self::source_signature(&provider)?)
            } else {
                None
            };
            self.execute(Pending {
                target: Target::Codex,
                changes,
                baseline: Some(baseline),
                active,
                provider_update: Some(provider.clone()),
                applied_summary: Some(applied_summary),
                accepted_source,
            })?;
        } else {
            let newest_source = Self::source_signature(&provider)?;
            let tx = self.conn.transaction()?;
            tx.execute(
                "UPDATE providers SET data=?1 WHERE id=?2",
                params![json(&provider)?, provider_id],
            )?;
            tx.execute(
                "UPDATE targets SET accepted_source=?1 WHERE active=?2 AND accepted_source=?3",
                params![newest_source, provider_id, previous_source],
            )?;
            tx.commit()?;
        }
        Ok(FastModeResult {
            provider: provider.summary,
            applied,
        })
    }

    pub fn rename_provider(&mut self, expected: Provider, name: String) -> Result<Provider> {
        self.recover()?;
        let mut stored = self.provider(&expected.id)?;
        if json(&stored.summary)? != json(&expected)? {
            return Err(AppError::new(
                "provider_changed",
                "供应商已更新，请刷新后重新命名；此次没有修改",
            ));
        }
        let name = name.trim().to_owned();
        if name.is_empty() || name.len() > 160 {
            return Err(AppError::new(
                "invalid_name",
                "名称为空或过长，请调整后保存",
            ));
        }
        if name == stored.summary.name {
            return Ok(stored.summary);
        }
        stored.summary.name = name;
        stored.summary.updated_at = now();
        self.conn.execute(
            "UPDATE providers SET data=?1 WHERE id=?2",
            params![json(&stored)?, stored.summary.id],
        )?;
        Ok(stored.summary)
    }

    pub fn protocol_probe_connection(
        &self,
        expected: &Provider,
    ) -> Result<(String, String, Option<CodexProtocol>, Option<String>)> {
        let stored = self.provider(&expected.id)?;
        if json(&stored.summary)? != json(expected)? {
            return Err(AppError::new(
                "provider_changed",
                "供应商已更新，请刷新后重新检测",
            ));
        }
        Ok((
            stored.summary.base_url,
            stored.api_key,
            stored.summary.codex_options.protocol_preference,
            stored.summary.codex_options.auth_preference,
        ))
    }

    pub fn accept_protocol_detection(
        &mut self,
        expected: Provider,
        result: ModelSyncResult,
    ) -> Result<Provider> {
        self.recover()?;
        let mut stored = self.provider(&expected.id)?;
        if json(&stored.summary)? != json(&expected)? {
            return Err(AppError::new(
                "provider_changed",
                "检测期间供应商已更新，请重新检测；此次没有修改",
            ));
        }
        validate_url(&result.base_url)?;
        stored.summary.base_url = result.base_url;
        stored.summary.auth_mode = result.auth_mode;
        let options = &mut stored.summary.codex_options;
        options.upstream_protocol = Some(result.protocol);
        options.protocol = result.protocol;
        options.claude_protocol = if result.protocol == CodexProtocol::Openai {
            ClaudeProtocol::Openai
        } else {
            options.fast_mode = None;
            ClaudeProtocol::Anthropic
        };
        options.protocol_detected_at = Some(result.synced_at);
        stored.summary.updated_at = now();
        self.conn.execute(
            "UPDATE providers SET data=?1 WHERE id=?2",
            params![json(&stored)?, stored.summary.id],
        )?;
        Ok(stored.summary)
    }

    pub fn set_protocol_conversion(
        &mut self,
        expected: Provider,
        target: Target,
        enabled: bool,
    ) -> Result<ProtocolConversionResult> {
        self.recover()?;
        let mut stored = self.provider(&expected.id)?;
        if json(&stored.summary)? != json(&expected)? {
            return Err(AppError::new(
                "provider_changed",
                "供应商已更新，请刷新后重新设置；此次没有写入",
            ));
        }
        if !stored.summary.needs_conversion(target) {
            return Err(AppError::new(
                "conversion_not_required",
                "此协议可直接接入当前客户端，无需转换",
            ));
        }
        if stored.summary.conversion_enabled(target) == enabled {
            return Ok(ProtocolConversionResult {
                provider: stored.summary,
                restored: false,
            });
        }
        let active = self.target_record(target)?.1.as_deref() == Some(&stored.summary.id);
        if active && self.status(target)?.state != "applied" {
            return Err(AppError::new(
                "configuration_changed",
                "请先处理此客户端待应用的修改或配置冲突，再调整转换开关；此次没有写入",
            ));
        }
        let disabled = &mut stored.summary.codex_options.conversion_disabled_targets;
        disabled.retain(|t| *t != target);
        if !enabled {
            disabled.push(target);
        }
        stored.summary.updated_at = now();
        let restored = active && !enabled;
        if restored {
            self.restore_with_provider(target, Some(stored.clone()))?;
        } else {
            self.conn.execute(
                "UPDATE providers SET data=?1 WHERE id=?2",
                params![json(&stored)?, stored.summary.id],
            )?;
        }
        Ok(ProtocolConversionResult {
            provider: stored.summary,
            restored,
        })
    }

    pub fn quick_model_settings(&mut self, input: QuickModelInput) -> Result<ModelWriteResult> {
        self.recover()?;
        let mut stored = self.provider(&input.expected.id)?;
        if json(&stored.summary)? != json(&input.expected)? {
            return Err(AppError::new(
                "provider_changed",
                "供应商已更新，请刷新列表后重新选择；此次没有写入",
            ));
        }
        let applied = self.target_record(input.target)?.1.as_deref() == Some(&stored.summary.id);
        if applied && self.status(input.target)?.state != "applied" {
            return Err(AppError::new(
                "configuration_changed",
                "此客户端有待应用的修改或配置冲突，请先更新或处理配置后再快捷调整；此次没有写入",
            ));
        }
        if !input.models.iter().any(|m| m.enabled) {
            return Err(AppError::new("no_enabled_models", "请至少启用一个模型"));
        }
        let mut options = stored.summary.codex_options.clone();
        options.models = input.models;
        if input.target == Target::Codex {
            for model in &mut options.models {
                model.context_window = Some(model.context_window.unwrap_or(256_000));
            }
            options.context_window = None;
            options.auto_compact_token_limit = None;
            if let Some(repair) = input.repair_reasoning_levels {
                options.repair_reasoning_levels = repair;
            }
        } else {
            // Editing Claude models must not overwrite Codex context choices.
            for model in &mut options.models {
                if let Some(saved) = stored
                    .summary
                    .codex_options
                    .models
                    .iter()
                    .find(|m| m.id == model.id)
                {
                    model.context_window = saved.context_window;
                }
            }
        }
        if let Some(synced_at) = input.synced_at {
            options.models_synced_at = Some(synced_at);
        }
        let mut validated = ProviderInput {
            id: Some(stored.summary.id.clone()),
            family: stored.summary.family,
            name: stored.summary.name.clone(),
            base_url: stored.summary.base_url.clone(),
            api_key: None,
            balance_access_token: None,
            model: input.model,
            auth_mode: stored.summary.auth_mode.clone(),
            reasoning_effort: stored.summary.reasoning_effort.clone(),
            codex_options: options,
        };
        validate(&mut validated)?;
        if stored.summary.model == validated.model
            && json(&stored.summary.codex_options)? == json(&validated.codex_options)?
        {
            if applied && input.target == Target::Codex {
                // Confirming unchanged model settings still checks display
                // metadata. An already complete catalog produces no revision.
                self.apply_stored(input.target, stored.clone(), None, true)?;
            }
            return Ok(ModelWriteResult {
                provider: stored.summary,
                applied,
            });
        }
        stored.summary.model = validated.model;
        stored.summary.codex_options = validated.codex_options;
        stored.summary.updated_at = now();
        if applied {
            self.apply_stored(input.target, stored.clone(), Some(stored.clone()), true)?;
        } else {
            self.conn.execute(
                "UPDATE providers SET data=?1 WHERE id=?2",
                params![json(&stored)?, stored.summary.id],
            )?;
        }
        Ok(ModelWriteResult {
            provider: stored.summary,
            applied,
        })
    }

    pub fn update_provider_models(&mut self, input: ModelWriteInput) -> Result<ModelWriteResult> {
        self.recover()?;
        let id = input
            .connection
            .provider_id
            .as_deref()
            .ok_or_else(|| AppError::new("missing_provider", "请先保存并应用供应商"))?;
        let saved = self.provider(id)?;
        if saved.summary.family != Family::Codex {
            return Err(AppError::new("wrong_family", "模型自动写入仅适用于 Codex"));
        }
        let (_, active, _) = self.target_record(Target::Codex)?;
        if active.as_deref() != Some(id) {
            return Ok(ModelWriteResult {
                provider: saved.summary,
                applied: false,
            });
        }
        if input.connection.base_url.trim().trim_end_matches('/') != saved.summary.base_url
            || input.auth_mode != saved.summary.auth_mode
            || input
                .connection
                .api_key
                .as_deref()
                .filter(|key| !key.trim().is_empty())
                .is_some_and(|key| key.trim() != saved.api_key)
        {
            return Err(AppError::new(
                "connection_changed",
                "连接信息已变更，请保存并应用后再同步到 Codex",
            ));
        }
        if self.status(Target::Codex)?.state != "applied" {
            return Err(AppError::new(
                "configuration_changed",
                "当前配置存在未应用或外部修改，请先处理后再自动写入模型",
            ));
        }
        if !input.models.iter().any(|m| m.enabled) {
            return Err(AppError::new("no_enabled_models", "请至少启用一个模型"));
        }
        let mut options = saved.summary.codex_options.clone();
        options.models = input.models;
        for model in &mut options.models {
            model.context_window = Some(model.context_window.unwrap_or(256_000));
        }
        options.models_synced_at = Some(input.synced_at);
        options.context_window = None;
        options.auto_compact_token_limit = None;
        let mut validated = ProviderInput {
            id: Some(id.to_owned()),
            family: Family::Codex,
            name: saved.summary.name.clone(),
            base_url: saved.summary.base_url.clone(),
            api_key: None,
            balance_access_token: None,
            model: input.model,
            auth_mode: saved.summary.auth_mode.clone(),
            reasoning_effort: saved.summary.reasoning_effort.clone(),
            codex_options: options,
        };
        validate(&mut validated)?;
        let mut updated = saved;
        updated.summary.model = validated.model;
        updated.summary.codex_options = validated.codex_options;
        updated.summary.updated_at = now();
        self.apply_stored(Target::Codex, updated.clone(), Some(updated.clone()), true)?;
        Ok(ModelWriteResult {
            provider: updated.summary,
            applied: true,
        })
    }

    pub fn restore(&mut self, target: Target) -> Result<TargetStatus> {
        self.restore_with_provider(target, None)
    }

    fn restore_with_provider(
        &mut self,
        target: Target,
        provider_update: Option<StoredProvider>,
    ) -> Result<TargetStatus> {
        self.recover()?;
        let (_, active, baseline) = self.target_record(target)?;
        let active_provider = active
            .as_deref()
            .map(|id| self.provider(id))
            .transpose()?
            .map(|p| Self::for_target(&p, target));
        let files = baseline
            .ok_or_else(|| AppError::new("nothing_to_restore", "此客户端尚未由 uni-switch 接管"))?;
        let changes = files
            .iter()
            .map(|file| {
                if adapters::changed_for_provider(file, active_provider.as_ref())? {
                    return adapters::restore(file);
                }
                let mut accepted = file.clone();
                accepted.expected = adapters::read(&file.path)?;
                adapters::restore(&accepted)
            })
            .collect::<Result<Vec<_>>>()?;
        self.execute(Pending {
            target,
            changes,
            baseline: None,
            active: None,
            provider_update,
            applied_summary: None,
            accepted_source: None,
        })?;
        self.status(target)
    }

    pub fn repair_reasoning_levels(&mut self, provider_id: &str) -> Result<ReasoningRepairResult> {
        self.recover()?;
        let mut provider = self.provider(provider_id)?;
        if provider.summary.family != Family::Codex {
            return Err(AppError::new("wrong_family", "此修复仅适用于 Codex"));
        }
        provider.summary.codex_options.repair_reasoning_levels = true;
        provider.summary.updated_at = now();
        if !provider
            .summary
            .codex_options
            .models
            .iter()
            .any(|m| m.enabled)
        {
            provider.summary.codex_options.models = vec![ProviderModel {
                id: provider.summary.model.clone(),
                context_window: None,
                reasoning_efforts: vec![],
                enabled: true,
            }];
        }
        let (directory, active, baseline) = self.target_record(Target::Codex)?;
        let applied = active.as_deref() == Some(provider_id);
        if !applied {
            self.conn.execute(
                "UPDATE providers SET data=?1 WHERE id=?2",
                params![json(&provider)?, provider_id],
            )?;
        } else {
            let mut baseline = baseline.ok_or_else(|| {
                AppError::new("invalid_state", "当前配置缺少管理记录，请重新应用后重试")
            })?;
            let saved = self.provider(provider_id)?;
            // Validate profile/credential compatibility without applying settings.
            adapters::plan(Target::Codex, &directory, &saved)?;
            for file in &baseline {
                if adapters::changed_for_provider(file, Some(&saved))? {
                    return Err(AppError::new("external_change", "当前 API 配置或模型目录已被其他工具修改，请先导入现有配置后重试；此次没有写入"));
                }
            }
            let files = adapters::reasoning_repair_files(&directory, &provider)?;
            let mut changes: Vec<Change> = files
                .iter()
                .filter(|f| f.original != f.expected)
                .map(|f| Change {
                    path: f.path.clone(),
                    before: f.original.clone(),
                    after: f.expected.clone(),
                })
                .collect();
            for file in files {
                if let Some(previous) = baseline.iter_mut().find(|old| old.path == file.path) {
                    if file.format == "toml" {
                        // Capture newly managed display preferences at the first repair,
                        // while retaining the original snapshots of existing managed keys.
                        let snapshot = ManagedFile {
                            expected: file.original.clone(),
                            original: previous.original.clone(),
                            keys: previous.keys.clone(),
                            ..file.clone()
                        };
                        previous.original = adapters::restore(&snapshot)?.after;
                        // Retain the applied model/effort defaults for status comparisons.
                        let mut expected = previous
                            .expected
                            .as_deref()
                            .unwrap_or("")
                            .parse::<toml_edit::DocumentMut>()
                            .map_err(|_| {
                                AppError::new("invalid_state", "已保存的 Codex 配置无效")
                            })?;
                        expected["model_catalog_json"] = toml_edit::value(
                            directory
                                .join("uni-switch-models.json")
                                .to_string_lossy()
                                .as_ref(),
                        );
                        let repaired = file
                            .expected
                            .as_deref()
                            .unwrap_or("")
                            .parse::<toml_edit::DocumentMut>()
                            .map_err(|_| {
                                AppError::new("invalid_state", "修复后的 Codex 配置无效")
                            })?;
                        if expected.get("desktop").is_none() {
                            expected["desktop"] = toml_edit::Item::Table(toml_edit::Table::new());
                        }
                        let display = repaired
                            .get("desktop")
                            .and_then(|v| v.get("enabled-reasoning-efforts"))
                            .ok_or_else(|| {
                                AppError::new("invalid_state", "修复后的思考强度显示设置缺失")
                            })?;
                        let desktop = expected
                            .get_mut("desktop")
                            .and_then(toml_edit::Item::as_table_like_mut)
                            .ok_or_else(|| {
                                AppError::new("invalid_state", "已保存的 Codex desktop 设置无效")
                            })?;
                        desktop.insert("enabled-reasoning-efforts", display.clone());
                        previous.expected = Some(expected.to_string());
                        if !previous.keys.iter().any(|k| k == "model_catalog_json") {
                            previous.keys.push("model_catalog_json".into());
                        }
                        if !previous
                            .keys
                            .iter()
                            .any(|key| key == adapters::REASONING_DISPLAY_KEY)
                        {
                            previous.keys.push(adapters::REASONING_DISPLAY_KEY.into());
                        }
                    } else {
                        previous.expected = file.expected;
                    }
                } else {
                    baseline.push(file);
                }
            }
            if let Some(mut applied_bridge) = self
                .bridge_provider_record()?
                .filter(|p| p.summary.id == provider_id)
            {
                applied_bridge.summary.codex_options.repair_reasoning_levels = true;
                applied_bridge.summary.updated_at = provider.summary.updated_at;
                if applied_bridge.summary.codex_options.models.is_empty() {
                    applied_bridge.summary.codex_options.models =
                        provider.summary.codex_options.models.clone();
                }
                let path = self.data_directory.join("bridge-active.json");
                let before = adapters::read(&path)?;
                let after = Some(json(&applied_bridge)?);
                if before != after {
                    changes.push(Change {
                        path: path.clone(),
                        before,
                        after: after.clone(),
                    });
                }
                if let Some(file) = baseline.iter_mut().find(|f| f.path == path) {
                    file.expected = after;
                }
            }
            self.execute(Pending {
                target: Target::Codex,
                changes,
                baseline: Some(baseline),
                active,
                provider_update: Some(provider.clone()),
                applied_summary: Some(provider.summary.clone()),
                accepted_source: Some(Self::source_signature(&provider)?),
            })?;
        }
        Ok(ReasoningRepairResult {
            provider: provider.summary,
            applied,
        })
    }

    fn execute(&mut self, pending: Pending) -> Result<()> {
        for change in &pending.changes {
            if adapters::read(&change.path)? != change.before {
                return Err(AppError::new(
                    "external_change",
                    "配置文件刚刚被其他程序修改，请刷新后重试",
                ));
            }
        }
        let id = uuid::Uuid::new_v4().to_string();
        let backups = self.data_directory.join("backups");
        writer::private_directory(&backups)?;
        writer::write(&backups.join(format!("{id}.json")), Some(&json(&pending)?))?;
        self.conn.execute(
            "INSERT INTO operations(id,status,data,created_at) VALUES(?1,'pending',?2,?3)",
            params![id, json(&pending)?, now()],
        )?;
        let applied = (|| {
            for change in &pending.changes {
                if adapters::read(&change.path)? != change.before {
                    return Err(AppError::new(
                        "external_change",
                        "写入前发现外部修改，正在恢复此次操作",
                    ));
                }
                writer::write(&change.path, change.after.as_deref())?;
                if adapters::read(&change.path)? != change.after {
                    return Err(AppError::new(
                        "verify_failed",
                        "配置文件回读校验失败，正在恢复此次操作",
                    ));
                }
            }
            self.finish(&id, &pending)
        })();
        if let Err(err) = applied {
            if self.rollback(&pending).is_err() {
                return Err(AppError::new("recovery_required", "部分文件无法恢复，已保留操作备份。请关闭客户端后重新打开 uni-switch；备份位于应用数据目录的 backups 文件夹"));
            }
            self.conn.execute(
                "UPDATE operations SET status='rolled_back',data='{}' WHERE id=?1",
                [&id],
            )?;
            return Err(err);
        }
        // Cleanup cannot turn an already committed application into a reported failure.
        let _ = self.cleanup_backups();
        Ok(())
    }

    fn finish(&mut self, id: &str, pending: &Pending) -> Result<()> {
        // Fast affects Codex only. If it is the sole changed setting, other
        // already-current clients keep their own snapshots and remain current.
        // Include this source advance in the same journaled database commit.
        let fast_transition = if let Some(updated) = &pending.provider_update {
            let old: Option<String> = self
                .conn
                .query_row(
                    "SELECT data FROM providers WHERE id=?1",
                    [&updated.summary.id],
                    |r| r.get(0),
                )
                .optional()?;
            if let Some(mut previous) = old.map(|data| decode_provider(&data)).transpose()? {
                let before = Self::source_signature(&previous)?;
                let changed = previous.summary.codex_options.fast_mode
                    != updated.summary.codex_options.fast_mode;
                previous.summary.codex_options.fast_mode = updated.summary.codex_options.fast_mode;
                let after = Self::source_signature(updated)?;
                if changed && Self::source_signature(&previous)? == after {
                    Some((updated.summary.id.clone(), before, after))
                } else {
                    None
                }
            } else {
                None
            }
        } else {
            None
        };
        let tx = self.conn.transaction()?;
        let baseline = pending.baseline.as_ref().map(json).transpose()?;
        let previous_revision: u64 = tx.query_row(
            "SELECT applied_at_ms FROM targets WHERE id=?1",
            [pending.target.id()],
            |r| r.get(0),
        )?;
        let changed = pending.changes.iter().any(|change| {
            pending.target != Target::Codex
                || change
                    .path
                    .file_name()
                    .and_then(|n| n.to_str())
                    .is_some_and(|n| matches!(n, "config.toml" | "uni-switch-models.json"))
        });
        let revision = if changed {
            (std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap_or_default()
                .as_millis() as u64)
                .max(previous_revision.saturating_add(1))
        } else {
            previous_revision
        };
        tx.execute(
            "UPDATE targets SET active=?1,baseline=?2,applied_at_ms=?4 WHERE id=?3",
            params![pending.active, baseline, pending.target.id(), revision],
        )?;
        tx.execute(
            "UPDATE targets SET applied_summary=?1,accepted_source=?2 WHERE id=?3",
            params![
                pending.applied_summary.as_ref().map(json).transpose()?,
                pending.accepted_source,
                pending.target.id()
            ],
        )?;
        if let Some(provider) = &pending.provider_update {
            tx.execute(
                "INSERT INTO providers(id,family,data) VALUES(?1,?2,?3) ON CONFLICT(id) DO UPDATE SET family=excluded.family,data=excluded.data",
                params![provider.summary.id, json(&provider.summary.family)?, json(provider)?],
            )?;
        }
        if let Some((provider_id, before, after)) = fast_transition {
            tx.execute("UPDATE targets SET accepted_source=?1 WHERE active=?2 AND accepted_source=?3 AND id!=?4", params![after, provider_id, before, pending.target.id()])?;
        }
        tx.execute(
            "UPDATE operations SET status='complete',data='{}' WHERE id=?1",
            [id],
        )?;
        tx.commit()?;
        Ok(())
    }

    fn rollback(&self, pending: &Pending) -> Result<()> {
        for change in pending.changes.iter().rev() {
            let current = adapters::read(&change.path)?;
            if current == change.before {
                continue;
            }
            if current != change.after {
                return Err(AppError::new(
                    "recovery_conflict",
                    "恢复时发现外部修改，已保留备份",
                ));
            }
            writer::write(&change.path, change.before.as_deref())?;
        }
        Ok(())
    }

    pub fn recover(&mut self) -> Result<()> {
        let rows: Vec<(String, String)> = {
            let mut stmt = self.conn.prepare(
                "SELECT id,data FROM operations WHERE status='pending' ORDER BY created_at",
            )?;
            let rows = stmt.query_map([], |row| Ok((row.get(0)?, row.get(1)?)))?;
            rows.collect::<std::result::Result<_, _>>()?
        };
        for (id, data) in rows {
            let pending: Pending = decode(&data)?;
            let mut all_after = true;
            for change in &pending.changes {
                if adapters::read(&change.path)? != change.after {
                    all_after = false;
                    break;
                }
            }
            if all_after {
                self.finish(&id, &pending)?;
            } else {
                self.rollback(&pending)?;
                self.conn.execute(
                    "UPDATE operations SET status='rolled_back',data='{}' WHERE id=?1",
                    [&id],
                )?;
            }
        }
        Ok(())
    }

    fn cleanup_backups(&self) -> Result<()> {
        let mut stmt = self.conn.prepare("SELECT id FROM operations WHERE status!='pending' ORDER BY created_at DESC,rowid DESC LIMIT -1 OFFSET 10")?;
        let ids = stmt
            .query_map([], |row| row.get::<_, String>(0))?
            .collect::<std::result::Result<Vec<_>, _>>()?;
        for id in ids {
            let path = self
                .data_directory
                .join("backups")
                .join(format!("{id}.json"));
            if path.is_file() {
                std::fs::remove_file(&path).map_err(|err| AppError::io(&path, err))?;
            }
            self.conn
                .execute("DELETE FROM operations WHERE id=?1", [&id])?;
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests;
