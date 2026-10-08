use crate::error::Result;

pub const SERVICE_WEBSITE: &str = "https://www.dieqiyun.top/";

/// A fixed first-party destination; supplier keys and addresses are never used.
pub fn open() -> Result<()> {
    #[cfg(feature = "qa-webview")]
    if let Some(marker) = std::env::var_os("UNI_SWITCH_QA_SERVICE_OPEN_MARKER") {
        std::fs::write(marker, SERVICE_WEBSITE)
            .map_err(|_| crate::error::AppError::new("service_open", "官网跳转记录未完成"))?;
        return Ok(());
    }
    crate::browser::open(
        SERVICE_WEBSITE,
        "service_open",
        "未能打开浏览器，请手动访问官网",
    )
}
