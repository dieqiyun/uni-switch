use crate::error::{AppError, Result};
use std::io::Write;
use std::path::Path;

pub fn private_directory(path: &Path) -> Result<()> {
    std::fs::create_dir_all(path).map_err(|err| AppError::io(path, err))?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o700))
            .map_err(|err| AppError::io(path, err))?;
    }
    #[cfg(windows)]
    private_permissions(path)?;
    Ok(())
}

#[cfg(windows)]
fn private_permissions(path: &Path) -> Result<()> {
    use std::os::windows::process::CommandExt;
    let user = std::env::var("USERNAME")
        .map_err(|_| AppError::new("permissions", "无法确定文件权限所属的用户"))?;
    let domain = std::env::var("USERDOMAIN").unwrap_or_default();
    let account = if domain.is_empty() {
        user
    } else {
        format!("{domain}\\{user}")
    };
    let grant = if path.is_dir() {
        format!("{account}:(OI)(CI)F")
    } else {
        format!("{account}:F")
    };
    let output = std::process::Command::new("icacls.exe")
        .arg(path)
        .args(["/inheritance:r", "/grant:r", &grant, "/Q"])
        .creation_flags(0x08000000)
        .output()
        .map_err(|err| AppError::io(path, err))?;
    if !output.status.success() {
        return Err(AppError::new(
            "permissions",
            format!("无法为 {} 设置当前用户私有权限", path.display()),
        ));
    }
    Ok(())
}

pub fn write(path: &Path, content: Option<&str>) -> Result<()> {
    if std::fs::symlink_metadata(path).is_ok_and(|m| m.file_type().is_symlink()) {
        return Err(AppError::new(
            "symlink",
            format!("{} 是符号链接，请直接选择实际配置目录", path.display()),
        ));
    }
    let Some(content) = content else {
        return match std::fs::remove_file(path) {
            Ok(()) => Ok(()),
            Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(()),
            Err(err) => Err(AppError::io(path, err)),
        };
    };
    let parent = path
        .parent()
        .ok_or_else(|| AppError::new("invalid_path", "配置文件缺少父目录"))?;
    std::fs::create_dir_all(parent).map_err(|err| AppError::io(parent, err))?;
    let mut temp =
        tempfile::NamedTempFile::new_in(parent).map_err(|err| AppError::io(parent, err))?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        temp.as_file()
            .set_permissions(std::fs::Permissions::from_mode(0o600))
            .map_err(|err| AppError::io(path, err))?;
    }
    #[cfg(windows)]
    private_permissions(temp.path())?;
    temp.write_all(content.as_bytes())
        .map_err(|err| AppError::io(path, err))?;
    temp.as_file()
        .sync_all()
        .map_err(|err| AppError::io(path, err))?;
    #[cfg(windows)]
    {
        use std::os::windows::ffi::OsStrExt;
        use windows_sys::Win32::Storage::FileSystem::{
            MoveFileExW, MOVEFILE_REPLACE_EXISTING, MOVEFILE_WRITE_THROUGH,
        };
        let source: Vec<u16> = temp
            .path()
            .as_os_str()
            .encode_wide()
            .chain(Some(0))
            .collect();
        let destination: Vec<u16> = path.as_os_str().encode_wide().chain(Some(0)).collect();
        if unsafe {
            MoveFileExW(
                source.as_ptr(),
                destination.as_ptr(),
                MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH,
            )
        } == 0
        {
            return Err(AppError::io(path, std::io::Error::last_os_error()));
        }
    }
    #[cfg(not(windows))]
    {
        temp.persist(path)
            .map_err(|err| AppError::io(path, err.error))?;
        std::fs::File::open(parent)
            .and_then(|file| file.sync_all())
            .map_err(|err| AppError::io(parent, err))?;
    }
    Ok(())
}
