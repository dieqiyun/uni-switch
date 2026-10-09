use super::*;
use crate::client_config::{
    self, ClientConfigStatus, ClientKind, ConfigDocument, ConfigFile, ConfigWriteResult,
    NativeProtocol,
};
#[cfg(test)]
mod tests;

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
struct ExtraState {
    active: Option<String>,
    baseline: Option<Vec<Change>>,
    revision: u64,
    #[serde(default)]
    manual: bool,
    source: Option<String>,
}
#[derive(Debug, Serialize, Deserialize)]
struct ConfigPending {
    client: ClientKind,
    changes: Vec<Change>,
    state: Option<ExtraState>,
    revision: u64,
}

impl Store {
    pub(super) fn initialize_client_configs(&mut self) -> Result<()> {
        self.conn.execute_batch("CREATE TABLE IF NOT EXISTS client_configs (id TEXT PRIMARY KEY, directory TEXT NOT NULL, selected INTEGER NOT NULL DEFAULT 0, state TEXT NOT NULL DEFAULT '{}'); CREATE TABLE IF NOT EXISTS config_operations (id TEXT PRIMARY KEY, status TEXT NOT NULL, data TEXT NOT NULL);")?;
        let columns = self
            .conn
            .prepare("PRAGMA table_info(targets)")?
            .query_map([], |r| r.get::<_, String>(1))?
            .collect::<std::result::Result<Vec<_>, _>>()?;
        if !columns.iter().any(|n| n == "configuration_manual") {
            self.conn.execute(
                "ALTER TABLE targets ADD COLUMN configuration_manual INTEGER NOT NULL DEFAULT 0",
                [],
            )?;
        }
        for client in [ClientKind::Zcode, ClientKind::Dsh, ClientKind::Workbuddy] {
            self.conn.execute(
                "INSERT OR IGNORE INTO client_configs(id,directory) VALUES(?1,?2)",
                params![
                    client.id(),
                    client_config::default_directory(client)?.to_string_lossy()
                ],
            )?;
        }
        self.recover_config_operations()
    }
    fn config_binding(&self, client: ClientKind) -> Result<(PathBuf, bool)> {
        if let Some(target) = client.target() {
            let (path, _, _) = self.target_record(target)?;
            return Ok((path, false));
        }
        self.conn
            .query_row(
                "SELECT directory,selected FROM client_configs WHERE id=?1",
                [client.id()],
                |r| Ok((PathBuf::from(r.get::<_, String>(0)?), !r.get::<_, bool>(1)?)),
            )
            .map_err(Into::into)
    }
    fn extra_state(&self, client: ClientKind) -> Result<ExtraState> {
        let state: String = self.conn.query_row(
            "SELECT state FROM client_configs WHERE id=?1",
            [client.id()],
            |r| r.get(0),
        )?;
        decode(&state)
    }
    fn config_specs(&self, client: ClientKind) -> Result<Vec<(PathBuf, &'static str)>> {
        if client.target().is_none() {
            if let Some(baseline) = self.extra_state(client)?.baseline {
                return Ok(baseline
                    .into_iter()
                    .map(|file| {
                        (
                            file.path,
                            if client == ClientKind::Dsh {
                                "yaml"
                            } else {
                                "json"
                            },
                        )
                    })
                    .collect());
            }
        }
        let (directory, default_binding) = self.config_binding(client)?;
        client_config::specifications(client, &directory, default_binding)
    }
    fn config_file(&self, client: ClientKind, file_id: &str) -> Result<(PathBuf, &'static str)> {
        self.config_specs(client)?
            .into_iter()
            .find(|(p, _)| client_config::revision(p, None) == file_id)
            .ok_or_else(|| {
                AppError::new(
                    "config_file_changed",
                    "文件不在当前客户端配置列表中，请重新打开编辑器",
                )
            })
    }
    pub fn client_config_status(&self, client: ClientKind) -> Result<ClientConfigStatus> {
        let (directory, _) = self.config_binding(client)?;
        let files = self
            .config_specs(client)?
            .into_iter()
            .map(|(p, f)| ConfigFile {
                id: client_config::revision(&p, None),
                path: p.to_string_lossy().into_owned(),
                format: f.into(),
                exists: p.is_file(),
            })
            .collect();
        let (active, restore, state, message) = if let Some(target) = client.target() {
            let status = self.status(target)?;
            (
                status.active_provider_id,
                status.can_restore,
                status.state,
                status.message,
            )
        } else {
            let stored = self.extra_state(client)?;
            let changed = stored.baseline.as_ref().is_some_and(|files| {
                files
                    .iter()
                    .any(|f| crate::adapters::read(&f.path).map_or(true, |v| v != f.after))
            });
            let state = if stored.manual {
                "manual_changes"
            } else if changed {
                "external_change"
            } else if stored.active.as_ref().is_some_and(|id| {
                self.provider(id)
                    .and_then(|p| Self::source_signature(&p))
                    .map_or(true, |signature| {
                        stored.source.as_deref() != Some(signature.as_str())
                    })
            }) {
                "saved_changes"
            } else if stored.active.is_some() {
                "applied"
            } else {
                "unmanaged"
            };
            (
                stored.active,
                stored.baseline.is_some(),
                state.into(),
                match state {
                    "manual_changes" => "配置已手动保存。重新应用供应商前需确认覆盖。",
                    "external_change" => "配置被外部修改，请查看文件后确认是否重新应用。",
                    "applied" => "配置已写入，请重启客户端并新开对话。",
                    "saved_changes" => "供应商已编辑，请重新一键配置以更新此客户端。",
                    _ => "选择已保存的供应商，写入此客户端的原生配置。",
                }
                .into(),
            )
        };
        Ok(ClientConfigStatus {
            client,
            directory: directory.to_string_lossy().into_owned(),
            files,
            active_provider_id: active,
            can_restore: restore,
            state,
            message,
            revision: self.config_snapshot(client)?,
        })
    }
    pub fn read_client_config(&self, client: ClientKind, file_id: &str) -> Result<ConfigDocument> {
        let (path, format) = self.config_file(client, file_id)?;
        client_config::check_path(&path)?;
        let content = crate::adapters::read(&path)?;
        if content.as_ref().is_some_and(|c| c.len() > 4 * 1024 * 1024) {
            return Err(AppError::new(
                "config_too_large",
                "配置文件超过 4 MiB，无法在应用内编辑",
            ));
        }
        Ok(ConfigDocument {
            client,
            file_id: file_id.into(),
            path: path.to_string_lossy().into_owned(),
            format: format.into(),
            revision: client_config::revision(&path, content.as_deref()),
            exists: content.is_some(),
            content: content.unwrap_or_else(|| {
                if format == "json" {
                    "{}\n".into()
                } else {
                    String::new()
                }
            }),
        })
    }
    pub fn save_client_config(&mut self, document: ConfigDocument) -> Result<ConfigWriteResult> {
        self.recover()?;
        let (path, format) = self.config_file(document.client, &document.file_id)?;
        client_config::check_path(&path)?;
        let before = crate::adapters::read(&path)?;
        if document.path != path.to_string_lossy()
            || document.revision != client_config::revision(&path, before.as_deref())
        {
            return Err(AppError::new(
                "config_conflict",
                "配置文件或目录在编辑期间发生变化。本次未保存，请保留编辑内容并重新读取文件",
            ));
        }
        client_config::validate_text(format, &document.content)?;
        if before.as_deref() == Some(document.content.as_str()) {
            let revision = if let Some(target) = document.client.target() {
                self.status(target)?.configuration_revision
            } else {
                self.extra_state(document.client)?.revision
            };
            return Ok(ConfigWriteResult {
                changed: false,
                backup_path: None,
                configuration_revision: revision,
            });
        }
        let state = if document.client.target().is_none() {
            let mut state = self.extra_state(document.client)?;
            state.manual = state.baseline.is_some();
            if let Some(files) = &mut state.baseline {
                if let Some(file) = files.iter_mut().find(|f| f.path == path) {
                    file.after = Some(document.content.clone());
                }
            }
            Some(state)
        } else {
            None
        };
        self.execute_config(ConfigPending {
            client: document.client,
            changes: vec![Change {
                path,
                before,
                after: Some(document.content),
            }],
            state,
            revision: 0,
        })
    }
    pub fn set_client_config_directory(
        &mut self,
        client: ClientKind,
        directory: String,
    ) -> Result<()> {
        self.recover()?;
        if let Some(target) = client.target() {
            return self.set_directory(target, directory);
        }
        let path = PathBuf::from(directory.trim());
        if !path.is_absolute() || path.is_file() {
            return Err(AppError::new(
                "invalid_path",
                "请填写客户端配置文件所在目录的绝对路径",
            ));
        }
        let (current, _) = self.config_binding(client)?;
        if path != current && self.extra_state(client)?.baseline.is_some() {
            return Err(AppError::new(
                "managed_directory",
                "请先恢复原配置，再更换目录",
            ));
        }
        self.conn.execute(
            "UPDATE client_configs SET directory=?1,selected=1 WHERE id=?2",
            params![path.to_string_lossy(), client.id()],
        )?;
        Ok(())
    }
    fn config_snapshot(&self, client: ClientKind) -> Result<String> {
        let snapshots = self
            .config_specs(client)?
            .iter()
            .map(|(p, _)| {
                client_config::check_path(p)?;
                Ok(client_config::revision(
                    p,
                    crate::adapters::read(p)?.as_deref(),
                ))
            })
            .collect::<Result<Vec<_>>>()?;
        Ok(client_config::revision(
            &self.config_binding(client)?.0,
            Some(&snapshots.join("\n")),
        ))
    }
    pub fn apply_client_config(
        &mut self,
        client: ClientKind,
        provider_id: &str,
        protocol: NativeProtocol,
        expected_revision: Option<&str>,
    ) -> Result<ConfigWriteResult> {
        self.recover()?;
        if client.target().is_some() {
            return Err(AppError::new(
                "invalid_client",
                "请使用原有客户端供应商入口",
            ));
        }
        let mut state = self.extra_state(client)?;
        if let Some(expected) = expected_revision {
            if expected != self.config_snapshot(client)? {
                return Err(AppError::new(
                    "config_conflict",
                    "配置在确认期间发生变化，请重新查看后确认覆盖",
                ));
            }
        } else if state.manual
            || state.baseline.as_ref().is_some_and(|files| {
                files
                    .iter()
                    .any(|f| crate::adapters::read(&f.path).map_or(true, |v| v != f.after))
            })
        {
            return Err(AppError::new(
                "config_conflict",
                "配置已手动或外部修改，请查看文件后确认覆盖",
            ));
        }
        let provider = self.provider(provider_id)?;
        let changes =
            client_config::plan(client, &self.config_specs(client)?, &provider, protocol)?;
        // Retain the exact original source; subsequent switches only advance expected contents.
        let baseline = changes
            .iter()
            .map(|c| Change {
                path: c.path.clone(),
                before: state
                    .baseline
                    .as_ref()
                    .and_then(|b| b.iter().find(|f| f.path == c.path))
                    .map(|f| f.before.clone())
                    .unwrap_or_else(|| c.before.clone()),
                after: c.after.clone(),
            })
            .collect();
        state.active = Some(provider_id.into());
        state.source = Some(Self::source_signature(&provider)?);
        state.baseline = Some(baseline);
        state.manual = false;
        self.execute_config(ConfigPending {
            client,
            changes,
            state: Some(state),
            revision: 0,
        })
    }
    pub fn restore_client_config(&mut self, client: ClientKind) -> Result<ConfigWriteResult> {
        self.recover()?;
        if let Some(target) = client.target() {
            let before = self.status(target)?.configuration_revision;
            let after = self.restore(target)?.configuration_revision;
            return Ok(ConfigWriteResult {
                changed: before != after,
                backup_path: None,
                configuration_revision: after,
            });
        }
        let state = self.extra_state(client)?;
        let baseline = state
            .baseline
            .ok_or_else(|| AppError::new("nothing_to_restore", "此客户端尚未接管"))?;
        let changes = baseline.iter().map(|c| {
            client_config::check_path(&c.path)?;
            let current = crate::adapters::read(&c.path)?;
            if current != c.after { return Err(AppError::new("config_conflict", "文件已被手动或外部修改，自动恢复已停止；请使用编辑器和操作备份手动保留或恢复内容")); }
            Ok(Change {path:c.path.clone(),before:current,after:c.before.clone()})
        }).collect::<Result<Vec<_>>>()?;
        self.execute_config(ConfigPending {
            client,
            changes,
            state: Some(ExtraState::default()),
            revision: 0,
        })
    }
    fn execute_config(&mut self, mut pending: ConfigPending) -> Result<ConfigWriteResult> {
        pending.changes.retain(|c| c.before != c.after);
        let previous = if let Some(target) = pending.client.target() {
            self.status(target)?.configuration_revision
        } else {
            self.extra_state(pending.client)?.revision
        };
        if pending.changes.is_empty() {
            if let Some(state) = &mut pending.state {
                state.revision = previous;
                self.conn.execute(
                    "UPDATE client_configs SET state=?1 WHERE id=?2",
                    params![json(state)?, pending.client.id()],
                )?;
            }
            return Ok(ConfigWriteResult {
                changed: false,
                backup_path: None,
                configuration_revision: previous,
            });
        }
        for change in &pending.changes {
            client_config::check_path(&change.path)?;
            if crate::adapters::read(&change.path)? != change.before {
                return Err(AppError::new(
                    "config_conflict",
                    "写入前发现外部修改，本次未保存",
                ));
            }
        }
        pending.revision = (SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_millis() as u64)
            .max(previous.saturating_add(1));
        if let Some(state) = &mut pending.state {
            state.revision = pending.revision;
        }
        let id = uuid::Uuid::new_v4().to_string();
        let backups = self.data_directory.join("backups");
        writer::private_directory(&backups)?;
        let backup = backups.join(format!("client-config-{id}.json"));
        writer::write(&backup, Some(&json(&pending)?))?;
        self.conn.execute(
            "INSERT INTO config_operations(id,status,data) VALUES(?1,'pending',?2)",
            params![id, json(&pending)?],
        )?;
        let applied = (|| {
            for change in &pending.changes {
                client_config::check_path(&change.path)?;
                if crate::adapters::read(&change.path)? != change.before {
                    return Err(AppError::new("config_conflict", "写入期间发现外部修改"));
                }
                writer::write(&change.path, change.after.as_deref())?;
                if crate::adapters::read(&change.path)? != change.after {
                    return Err(AppError::new("verify_failed", "保存后回读校验失败"));
                }
            }
            self.finish_config(&id, &pending)
        })();
        if let Err(error) = applied {
            self.rollback_config(&pending)?;
            self.conn.execute(
                "UPDATE config_operations SET status='rolled_back',data='{}' WHERE id=?1",
                [&id],
            )?;
            return Err(error);
        }
        Ok(ConfigWriteResult {
            changed: true,
            backup_path: Some(backup.to_string_lossy().into_owned()),
            configuration_revision: pending.revision,
        })
    }
    fn finish_config(&mut self, id: &str, pending: &ConfigPending) -> Result<()> {
        let tx = self.conn.transaction()?;
        if let Some(target) = pending.client.target() {
            tx.execute(
                "UPDATE targets SET applied_at_ms=?1,configuration_manual=1 WHERE id=?2",
                params![pending.revision, target.id()],
            )?;
        } else if let Some(state) = &pending.state {
            tx.execute(
                "UPDATE client_configs SET state=?1 WHERE id=?2",
                params![json(state)?, pending.client.id()],
            )?;
        }
        tx.execute(
            "UPDATE config_operations SET status='complete',data='{}' WHERE id=?1",
            [id],
        )?;
        tx.commit()?;
        Ok(())
    }
    fn rollback_config(&self, pending: &ConfigPending) -> Result<()> {
        for change in pending.changes.iter().rev() {
            client_config::check_path(&change.path)?;
            let current = crate::adapters::read(&change.path)?;
            if current == change.before {
                continue;
            }
            if current != change.after {
                return Err(AppError::new(
                    "recovery_conflict",
                    "恢复时发现外部修改。操作备份已保留，请检查应用数据目录中的 backups",
                ));
            }
            writer::write(&change.path, change.before.as_deref())?;
        }
        Ok(())
    }
    pub(super) fn recover_config_operations(&mut self) -> Result<()> {
        let rows = self
            .conn
            .prepare("SELECT id,data FROM config_operations WHERE status='pending' ORDER BY rowid")?
            .query_map([], |r| Ok((r.get::<_, String>(0)?, r.get::<_, String>(1)?)))?
            .collect::<std::result::Result<Vec<_>, _>>()?;
        for (id, data) in rows {
            let pending: ConfigPending = decode(&data)?;
            if pending
                .changes
                .iter()
                .all(|c| crate::adapters::read(&c.path).is_ok_and(|v| v == c.after))
            {
                self.finish_config(&id, &pending)?;
            } else {
                self.rollback_config(&pending)?;
                self.conn.execute(
                    "UPDATE config_operations SET status='rolled_back',data='{}' WHERE id=?1",
                    [id],
                )?;
            }
        }
        Ok(())
    }
    pub(super) fn manually_configured(&self, target: Target) -> Result<bool> {
        self.conn
            .query_row(
                "SELECT configuration_manual FROM targets WHERE id=?1",
                [target.id()],
                |r| r.get(0),
            )
            .map_err(Into::into)
    }
    pub(super) fn extra_client_uses(&self, id: &str) -> Result<bool> {
        for client in [ClientKind::Zcode, ClientKind::Dsh, ClientKind::Workbuddy] {
            if self.extra_state(client)?.active.as_deref() == Some(id) {
                return Ok(true);
            }
        }
        Ok(false)
    }
}
