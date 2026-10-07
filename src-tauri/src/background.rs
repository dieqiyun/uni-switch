use crate::error::{AppError, Result};
use serde::Serialize;

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct BackgroundSettings {
    pub supported: bool,
    pub enabled: bool,
}

fn isolated() -> bool {
    std::env::var_os("UNI_SWITCH_DATA_DIR").is_some()
}
pub fn settings() -> Result<BackgroundSettings> {
    if isolated() {
        return Ok(BackgroundSettings {
            supported: false,
            enabled: false,
        });
    }
    #[cfg(windows)]
    {
        Ok(BackgroundSettings {
            supported: true,
            enabled: windows::enabled()?,
        })
    }
    #[cfg(not(windows))]
    {
        Ok(BackgroundSettings {
            supported: false,
            enabled: false,
        })
    }
}
pub fn set(enabled: bool) -> Result<BackgroundSettings> {
    if isolated() {
        return Err(AppError::new("qa_isolation", "隔离测试不会修改系统启动项"));
    }
    #[cfg(windows)]
    {
        windows::set(enabled)?;
        settings()
    }
    #[cfg(not(windows))]
    {
        let _ = enabled;
        Err(AppError::new(
            "unsupported_platform",
            "当前平台暂不支持系统后台启动",
        ))
    }
}

#[cfg(windows)]
mod windows {
    use super::*;
    use windows_sys::Win32::System::Registry::*;
    fn wide(value: &str) -> Vec<u16> {
        value.encode_utf16().chain(Some(0)).collect()
    }
    struct Key(HKEY);
    impl Drop for Key {
        fn drop(&mut self) {
            unsafe {
                RegCloseKey(self.0);
            }
        }
    }
    fn open(write: bool) -> Result<Option<Key>> {
        let mut key = std::ptr::null_mut();
        let path = wide("Software\\Microsoft\\Windows\\CurrentVersion\\Run");
        let status = if write {
            unsafe {
                RegCreateKeyExW(
                    HKEY_CURRENT_USER,
                    path.as_ptr(),
                    0,
                    std::ptr::null(),
                    REG_OPTION_NON_VOLATILE,
                    KEY_QUERY_VALUE | KEY_SET_VALUE,
                    std::ptr::null(),
                    &mut key,
                    std::ptr::null_mut(),
                )
            }
        } else {
            unsafe {
                RegOpenKeyExW(
                    HKEY_CURRENT_USER,
                    path.as_ptr(),
                    0,
                    KEY_QUERY_VALUE,
                    &mut key,
                )
            }
        };
        if !write && status == 2 {
            return Ok(None);
        }
        if status != 0 {
            return Err(AppError::new(
                "startup_permission",
                "无法访问当前用户的启动项，请检查系统权限",
            ));
        }
        Ok(Some(Key(key)))
    }
    fn command() -> Result<String> {
        let exe = std::env::current_exe()
            .map_err(|_| AppError::new("startup_path", "无法确定应用位置"))?;
        Ok(format!("\"{}\" --background", exe.to_string_lossy()))
    }
    pub fn enabled() -> Result<bool> {
        let Some(key) = open(false)? else {
            return Ok(false);
        };
        let mut bytes = 0;
        let mut kind = 0;
        let name = wide("uni-switch");
        let status = unsafe {
            RegQueryValueExW(
                key.0,
                name.as_ptr(),
                std::ptr::null(),
                &mut kind,
                std::ptr::null_mut(),
                &mut bytes,
            )
        };
        if status == 2 {
            return Ok(false);
        }
        if status != 0 || bytes > 32768 {
            return Err(AppError::new("startup_read", "系统启动项无法读取"));
        }
        if kind != REG_SZ {
            return Ok(false);
        }
        let mut data = vec![0u16; (bytes as usize).div_ceil(2)];
        let status = unsafe {
            RegQueryValueExW(
                key.0,
                name.as_ptr(),
                std::ptr::null(),
                &mut kind,
                data.as_mut_ptr().cast(),
                &mut bytes,
            )
        };
        if status != 0 {
            return Err(AppError::new("startup_read", "系统启动项无法读取"));
        }
        let value = String::from_utf16_lossy(&data)
            .trim_end_matches('\0')
            .to_owned();
        Ok(value == command()?)
    }
    pub fn set(enabled: bool) -> Result<()> {
        let key =
            open(true)?.ok_or_else(|| AppError::new("startup_write", "系统启动项无法访问"))?;
        let name = wide("uni-switch");
        let status = if enabled {
            let value = wide(&command()?);
            unsafe {
                RegSetValueExW(
                    key.0,
                    name.as_ptr(),
                    0,
                    REG_SZ,
                    value.as_ptr().cast(),
                    (value.len() * 2) as u32,
                )
            }
        } else {
            unsafe { RegDeleteValueW(key.0, name.as_ptr()) }
        };
        if status != 0 && !(status == 2 && !enabled) {
            return Err(AppError::new(
                "startup_write",
                "后台启动设置未保存，请检查系统权限",
            ));
        }
        Ok(())
    }
}
