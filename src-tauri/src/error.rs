use serde::Serialize;

#[derive(Debug, thiserror::Error, Serialize)]
#[error("{message}")]
#[serde(rename_all = "camelCase")]
pub struct AppError {
    pub code: String,
    pub message: String,
}

impl AppError {
    pub fn new(code: &str, message: impl Into<String>) -> Self {
        Self {
            code: code.to_owned(),
            message: message.into(),
        }
    }
    pub fn io(path: &std::path::Path, err: std::io::Error) -> Self {
        Self::new(
            "file_error",
            format!("无法访问 {}：{}", path.display(), err),
        )
    }
}

impl From<rusqlite::Error> for AppError {
    fn from(_: rusqlite::Error) -> Self {
        Self::new(
            "database_error",
            "本地数据库操作失败，请检查数据目录权限和磁盘空间",
        )
    }
}

pub type Result<T> = std::result::Result<T, AppError>;
