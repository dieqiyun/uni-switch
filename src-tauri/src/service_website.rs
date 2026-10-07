use crate::error::{AppError, Result};

pub const SERVICE_WEBSITE: &str = "https://www.dieqiyun.top/";

/// A fixed first-party destination; supplier keys and addresses are never used.
pub fn open() -> Result<()> {
    #[cfg(feature = "qa-webview")]
    if let Some(marker) = std::env::var_os("UNI_SWITCH_QA_SERVICE_OPEN_MARKER") {
        std::fs::write(marker, SERVICE_WEBSITE)
            .map_err(|_| AppError::new("service_open", "官网跳转记录未完成"))?;
        return Ok(());
    }
    #[cfg(windows)]
    {
        use windows_sys::Win32::UI::{Shell::ShellExecuteW, WindowsAndMessaging::SW_SHOWNORMAL};
        let operation: Vec<u16> = "open\0".encode_utf16().collect();
        let target: Vec<u16> = SERVICE_WEBSITE.encode_utf16().chain(Some(0)).collect();
        let result = unsafe {
            ShellExecuteW(
                std::ptr::null_mut(),
                operation.as_ptr(),
                target.as_ptr(),
                std::ptr::null(),
                std::ptr::null(),
                SW_SHOWNORMAL,
            )
        };
        if result as isize <= 32 {
            return Err(AppError::new(
                "service_open",
                "未能打开浏览器，请手动访问官网",
            ));
        }
        Ok(())
    }
    #[cfg(not(windows))]
    Err(AppError::new("service_open", "请手动在浏览器中打开官网"))
}
