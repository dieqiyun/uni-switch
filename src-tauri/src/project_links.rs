use crate::error::{AppError, Result};
use serde::Deserialize;

#[derive(Debug, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ProjectPage {
    Source,
    License,
}

impl ProjectPage {
    pub fn url(&self) -> &'static str {
        match self {
            Self::Source => "https://github.com/dieqiyun/uni-switch",
            Self::License => "https://github.com/dieqiyun/uni-switch/blob/main/LICENSE",
        }
    }
}

/// Only fixed project destinations are accepted; no supplier data is transmitted.
pub fn open(page: ProjectPage) -> Result<()> {
    let url = page.url();
    #[cfg(feature = "qa-webview")]
    if let Some(marker) = std::env::var_os("UNI_SWITCH_QA_PROJECT_OPEN_MARKER") {
        std::fs::write(marker, url)
            .map_err(|_| AppError::new("project_open", "项目链接记录未完成"))?;
        return Ok(());
    }
    #[cfg(windows)]
    {
        use windows_sys::Win32::UI::{Shell::ShellExecuteW, WindowsAndMessaging::SW_SHOWNORMAL};
        let operation: Vec<u16> = "open\0".encode_utf16().collect();
        let target: Vec<u16> = url.encode_utf16().chain(Some(0)).collect();
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
                "project_open",
                "未能打开浏览器，请手动访问项目页面",
            ));
        }
        Ok(())
    }
    #[cfg(not(windows))]
    {
        let _ = url;
        Err(AppError::new(
            "project_open",
            "请手动在浏览器中打开项目页面",
        ))
    }
}
