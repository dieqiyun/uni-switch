use super::*;
use crate::adapters::Change;
#[cfg(test)]
use crate::client_config;
use crate::client_config::{
    ClientConfigStatus, ClientKind, ConfigDocument, ConfigWriteResult, NativeProtocol,
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
    // Last generated WorkBuddy delta; manual saves must not acquire ownership.
    workbuddy_routes: Option<Change>,
}
#[cfg(test)]
#[derive(Debug, Serialize, Deserialize)]
struct ConfigPending {
    client: ClientKind,
    changes: Vec<Change>,
    state: Option<ExtraState>,
    revision: u64,
}

impl Store {
    pub(super) fn initialize_client_configs(&mut self) -> Result<()> {
        let _: bool = self.config_call("initialize", serde_json::json!({}))?;
        Ok(())
    }
    #[cfg(test)]
    fn config_specs(&self, client: ClientKind) -> Result<Vec<(PathBuf, &'static str)>> {
        #[derive(Deserialize)]
        struct Spec {
            path: PathBuf,
            format: String,
        }
        let files: Vec<Spec> =
            self.config_call("specifications", serde_json::json!({"client":client}))?;
        files
            .into_iter()
            .map(|s| Ok((s.path, adapters::format_name(&s.format)?)))
            .collect()
    }
    pub fn client_config_status(&self, client: ClientKind) -> Result<ClientConfigStatus> {
        self.config_call("clientStatus", serde_json::json!({"client":client}))
    }
    pub fn read_client_config(&self, client: ClientKind, file_id: &str) -> Result<ConfigDocument> {
        self.config_call(
            "read",
            serde_json::json!({"client":client,"fileId":file_id}),
        )
    }
    pub fn save_client_config(&mut self, document: ConfigDocument) -> Result<ConfigWriteResult> {
        self.config_call("save", serde_json::json!({"document":document}))
    }
    pub fn set_client_config_directory(
        &mut self,
        client: ClientKind,
        directory: String,
    ) -> Result<()> {
        let _: bool = self.config_call(
            "setDirectory",
            serde_json::json!({"client":client,"path":directory}),
        )?;
        Ok(())
    }
    pub fn apply_client_config(
        &mut self,
        client: ClientKind,
        provider_id: &str,
        protocol: NativeProtocol,
        expected_revision: Option<&str>,
    ) -> Result<ConfigWriteResult> {
        self.config_call("applyClient",serde_json::json!({"client":client,"providerId":provider_id,"protocol":protocol,"revision":expected_revision}))
    }
    pub fn set_client_config_profile(&mut self,client:ClientKind,profile:String)->Result<()> {
        let _:bool=self.config_call("setProfile",serde_json::json!({"client":client,"profile":profile}))?;
        Ok(())
    }
    pub fn restore_client_config(&mut self, client: ClientKind) -> Result<ConfigWriteResult> {
        self.config_call("restoreClient", serde_json::json!({"client":client}))
    }
    #[cfg(test)]
    fn rollback_config(&self, pending: &ConfigPending) -> Result<()> {
        writer::rollback(&pending.changes, crate::client_config::check_path)
    }
    #[cfg(test)]
    pub(super) fn recover_config_operations(&mut self) -> Result<()> {
        self.recover()
    }
    fn extra_state(&self, client: ClientKind) -> Result<ExtraState> {
        self.config_call("extraState", serde_json::json!({"client":client}))
    }
    pub(super) fn extra_client_uses(&self, id: &str) -> Result<bool> {
        for &c in ClientKind::EXTRAS {
            if self.extra_state(c)?.active.as_deref() == Some(id) {
                return Ok(true);
            }
        }
        Ok(false)
    }
}
