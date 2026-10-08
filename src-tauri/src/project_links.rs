use crate::error::Result;
use serde::Deserialize;

#[derive(Debug, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ProjectPage {
    Source,
    License,
    Tutorial,
}

impl ProjectPage {
    pub fn url(&self) -> &'static str {
        match self {
            Self::Source => "https://github.com/dieqiyun/uni-switch",
            Self::Tutorial => "https://github.com/dieqiyun/uni-switch/blob/main/docs/tutorial.md",
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
            .map_err(|_| crate::error::AppError::new("project_open", "项目链接记录未完成"))?;
        return Ok(());
    }
    crate::browser::open(url, "project_open", "未能打开浏览器，请手动访问项目页面")
}
