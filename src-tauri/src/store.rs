#[cfg(test)]
use crate::adapters::Change;
use crate::adapters::{self, ManagedFile};
use crate::error::{AppError, Result};
use crate::go_state::Connection;
use crate::types::*;
use crate::writer;
use rusqlite::{params, OptionalExtension};
use serde::{Deserialize, Serialize};
use std::path::PathBuf;
use std::time::{SystemTime, UNIX_EPOCH};
mod client_configs;

pub struct Store {
    conn: Connection,
    pub data_directory: PathBuf,
    _lock: std::fs::File,
    bridge_route: Option<crate::bridge::Route>,
    repaired_model_capabilities: bool,
}

#[cfg(test)]
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
    let base = provider.summary.base_url.clone();
    let context = provider.summary.codex_options.context_window;
    crate::model_registry::resolve_models(
        &mut provider.summary.codex_options.models,
        &base,
        context,
    )?;
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
            repaired_model_capabilities: false,
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
        store.initialize_client_configs()?;
        store.recover()?;
        store.migrate_model_capabilities()?;
        Ok(store)
    }

    fn migrate_model_capabilities(&mut self) -> Result<()> {
        self.repaired_model_capabilities = self.config_call("migrate", serde_json::json!({}))?;
        Ok(())
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

    #[cfg(test)]
    fn bridge_provider_record(&self) -> Result<Option<StoredProvider>> {
        self.bridge_record(Target::Codex)
    }

    fn bridge_record(&self, target: Target) -> Result<Option<StoredProvider>> {
        self.config_call("bridgeRecord", serde_json::json!({"target":target}))
    }

    #[cfg(test)]
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

    #[cfg(test)]
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

    fn config_call<T: serde::de::DeserializeOwned>(
        &self,
        op: &str,
        mut params: serde_json::Value,
    ) -> Result<T> {
        params["directory"] = serde_json::json!(self.data_directory);
        params["cacheDirectory"] = serde_json::json!(crate::model_registry::cache_directory());
        params["route"] = serde_json::json!(self.bridge_route);
        let v = crate::metadata_transport::call(&format!("configuration.{op}"), params)?;
        serde_json::from_value(v).map_err(|_| AppError::new("go_config", "Go 配置服务响应格式无效"))
    }
    #[cfg(test)]
    fn plan(
        &self,
        target: Target,
        _directory: &std::path::Path,
        provider: &StoredProvider,
    ) -> Result<Vec<ManagedFile>> {
        self.config_call(
            "plan",
            serde_json::json!({"target":target,"provider":provider}),
        )
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
    #[cfg(test)]
    fn source_signature(provider: &StoredProvider) -> Result<String> {
        adapters::call("signature", serde_json::json!({"provider":provider}))
    }
    pub fn sync_connection(&mut self, target: Target, provider_id: &str) -> Result<TargetStatus> {
        let _: bool = self.config_call(
            "sync",
            serde_json::json!({"target":target,"providerId":provider_id}),
        )?;
        self.status(target)
    }
    pub fn delete(&mut self, id: &str) -> Result<()> {
        let in_use: bool = self.conn.query_row(
            "SELECT EXISTS(SELECT 1 FROM targets WHERE active=?1)",
            [id],
            |row| row.get(0),
        )?;
        if in_use || self.extra_client_uses(id)? {
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
        self.config_call("status", serde_json::json!({"target":target}))
    }

    pub fn overview(&self) -> Result<Overview> {
        Ok(Overview {
            providers: self.list()?,
            targets: [Target::Codex, Target::ClaudeDesktop, Target::ClaudeCli]
                .into_iter()
                .map(|t| self.status(t))
                .collect::<Result<_>>()?,
            data_directory: self.data_directory.to_string_lossy().into_owned(),
            repaired_model_capabilities: self.repaired_model_capabilities,
        })
    }

    pub fn set_directory(&mut self, target: Target, directory: String) -> Result<()> {
        let _: bool = self.config_call(
            "setDirectory",
            serde_json::json!({"client":target.id(),"path":directory}),
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
        let _:bool=self.config_call("setDirectory",serde_json::json!({"client":target.id(),"path":directory,"expected":expected,"automatic":automatic}))?;
        Ok(())
    }
    pub fn apply(&mut self, target: Target, provider_id: &str) -> Result<TargetStatus> {
        let _: bool = self.config_call(
            "apply",
            serde_json::json!({"target":target,"providerId":provider_id}),
        )?;
        self.status(target)
    }
    pub fn prepare_apply_overwrite(
        &mut self,
        target: Target,
        provider_id: &str,
    ) -> Result<ApplyOverwriteConfirmation> {
        self.config_call(
            "prepare",
            serde_json::json!({"target":target,"providerId":provider_id}),
        )
    }
    pub fn apply_overwrite(
        &mut self,
        target: Target,
        provider_id: &str,
        token: &str,
    ) -> Result<TargetStatus> {
        let _: bool = self.config_call(
            "confirm",
            serde_json::json!({"target":target,"providerId":provider_id,"token":token}),
        )?;
        self.status(target)
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
        let _:bool=self.config_call("applyStored",serde_json::json!({"target":target,"provider":provider,"providerUpdate":provider_update,"preserve":preserve_choices,"source":source}))?;
        self.status(target)
    }

    pub fn set_provider_fast_mode(
        &mut self,
        provider_id: &str,
        enabled: bool,
    ) -> Result<FastModeResult> {
        self.config_call(
            "fast",
            serde_json::json!({"providerId":provider_id,"enabled":enabled}),
        )
    }
    pub fn rename_provider(&mut self, expected: Provider, name: String) -> Result<Provider> {
        self.recover()?;
        let mut stored = self.provider(&expected.id)?;
        if serde_json::to_value(&stored.summary).ok() != serde_json::to_value(&expected).ok() {
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
        if serde_json::to_value(&stored.summary).ok() != serde_json::to_value(expected).ok() {
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
        if serde_json::to_value(&stored.summary).ok() != serde_json::to_value(&expected).ok() {
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
        if serde_json::to_value(&stored.summary).ok() != serde_json::to_value(&expected).ok() {
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
        if serde_json::to_value(&stored.summary).ok() != serde_json::to_value(&input.expected).ok()
        {
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
        let _: bool = self.config_call(
            "restore",
            serde_json::json!({"target":target,"providerUpdate":provider_update}),
        )?;
        self.status(target)
    }

    pub fn repair_reasoning_levels(&mut self, provider_id: &str) -> Result<ReasoningRepairResult> {
        self.config_call("repair", serde_json::json!({"providerId":provider_id}))
    }

    #[cfg(test)]
    fn rollback(&self, pending: &Pending) -> Result<()> {
        writer::rollback(&pending.changes, |_| Ok(()))
    }
    pub fn recover(&mut self) -> Result<()> {
        let _: bool = self.config_call("recover", serde_json::json!({}))?;
        Ok(())
    }
}

#[cfg(test)]
mod tests;
