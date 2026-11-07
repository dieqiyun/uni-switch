use crate::{
    adapters::Change,
    error::Result,
    types::{StoredProvider, Target},
};
use serde::{Deserialize, Serialize};
use serde_json::json;
use std::path::{Path, PathBuf};

include!("client_kind.rs");
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum NativeProtocol {
 Gemini,
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
#[derive(Debug, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ClientConfigStatus {
	#[serde(default)]
	pub profiles: Vec<ClientLocation>,
	#[serde(default)]
	pub profile: String,
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
pub struct ClientLocation { pub id: String, pub name: String, pub directory: String }
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
#[derive(Debug, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ConfigWriteResult {
 #[serde(skip_serializing_if = "Option::is_none")]
 pub import_link: Option<String>,
    pub changed: bool,
    pub backup_path: Option<String>,
    pub configuration_revision: u64,
}

pub fn default_directory(client: ClientKind) -> Result<PathBuf> {
    crate::adapters::call("defaultDirectory", json!({"client":client}))
}
pub fn specifications(
    client: ClientKind,
    directory: &Path,
    default_binding: bool,
) -> Result<Vec<(PathBuf, &'static str)>> {
    #[derive(Deserialize)]
    struct Spec {
        path: PathBuf,
        format: String,
    }
    let specs: Vec<Spec> = crate::adapters::call(
        "specifications",
        json!({"client":client,"directory":directory,"defaultBinding":default_binding,"editor":true}),
    )?;
    specs
        .into_iter()
        .map(|s| Ok((s.path, crate::adapters::format_name(&s.format)?)))
        .collect()
}
pub fn check_path(path: &Path) -> Result<()> {
    let _: bool = crate::adapters::call("checkPath", json!({"path":path}))?;
    Ok(())
}
pub fn revision(path: &Path, content: Option<&str>) -> String {
    crate::adapters::call("revision", json!({"path":path,"content":content}))
        .expect("Go client revision service unavailable")
}
pub fn validate_text(format: &str, text: &str) -> Result<()> {
    let _: bool = crate::adapters::call("validate", json!({"format":format,"content":text}))?;
    Ok(())
}
#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ConfigPlan {
    pub changes: Vec<Change>,
    pub workbuddy_routes: Option<Change>,
}
pub fn plan(
    client: ClientKind,
    specs: &[(PathBuf, &'static str)],
    provider: &StoredProvider,
    protocol: NativeProtocol,
    previous: Option<&Change>,
) -> Result<ConfigPlan> {
    let files=specs.iter().map(|(path,format)|json!({"path":path,"format":format,"keys":[],"original":null,"expected":null})).collect::<Vec<_>>();
    crate::adapters::call(
        "extraPlan",
        json!({"client":client,"files":files,"provider":provider,"protocol":protocol,"previous":previous}),
    )
}
