//! Data-only file transport; Go owns writes, path checks and rollback.
use crate::error::Result;
use serde_json::json;
use std::path::Path;
pub fn private_directory(path: &Path) -> Result<()> {
    crate::metadata_transport::call("files.privateDirectory", json!({"path":path}))?;
    Ok(())
}
pub fn write(path: &Path, content: Option<&str>) -> Result<()> {
    crate::metadata_transport::call("files.write", json!({"path":path,"content":content}))?;
    Ok(())
}
#[cfg(test)]
pub(crate) fn rollback(
    changes: &[crate::adapters::Change],
    check_path: impl Fn(&Path) -> Result<()>,
) -> Result<()> {
    let _ = check_path;
    crate::metadata_transport::call("files.rollback", json!({"changes":changes}))?;
    Ok(())
}
