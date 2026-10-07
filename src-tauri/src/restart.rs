//! Restart identified desktop clients and assist CLI restarts without killing
//! an interactive terminal or replaying its original command line.
use crate::error::{AppError, Result};
use crate::types::Target;
use serde::Serialize;
use std::path::Path;

#[cfg(windows)]
mod windows;

#[derive(Debug, Default, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct DesktopRuntime {
    pub running: bool,
    pub restart_required: bool,
    pub can_restart: bool,
    pub reason: Option<String>,
    pub restart_in_progress: bool,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct RestartResult {
    pub restarted: bool,
    pub message: String,
    pub pending: bool,
}

pub fn desktop_runtime(directory: &Path, revision: u64) -> DesktopRuntime {
    client_runtime(Target::Codex, directory, revision)
}

pub fn client_runtime(target: Target, directory: &Path, revision: u64) -> DesktopRuntime {
    #[cfg(windows)]
    {
        windows::client_runtime(target, directory, revision)
    }
    #[cfg(not(windows))]
    {
        let _ = (target, directory, revision);
        DesktopRuntime::default()
    }
}

pub fn restart_codex(directory: &Path) -> Result<RestartResult> {
    restart_client(Target::Codex, directory)
}

pub fn restart_client(target: Target, directory: &Path) -> Result<RestartResult> {
    #[cfg(windows)]
    {
        windows::restart_client(target, directory)
    }
    #[cfg(not(windows))]
    {
        let _ = (target, directory);
        Err(AppError::new(
            "restart_unsupported",
            "此平台暂不支持自动重启，请退出对应客户端后重新打开",
        ))
    }
}
