use crate::error::{AppError, Result};
use serde::{Deserialize, Serialize};
use std::time::{Duration, SystemTime, UNIX_EPOCH};
use url::Url;

const MAX_RELEASE_BYTES: usize = 512 * 1024;

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct ReleaseConfig {
    github_repository: Option<String>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct UpdateSource {
    pub current_version: String,
    pub repository: Option<String>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct UpdateCheck {
    pub current_version: String,
    pub latest_version: String,
    pub available: bool,
    pub repository: String,
    pub release_url: String,
    pub download_url: Option<String>,
    pub notes: String,
    pub published_at: Option<String>,
    pub checked_at: u64,
}

#[derive(Deserialize)]
struct ReleaseAsset {
    name: String,
    browser_download_url: String,
}

#[derive(Deserialize)]
struct Release {
    tag_name: String,
    html_url: String,
    #[serde(default)]
    draft: bool,
    #[serde(default)]
    prerelease: bool,
    body: Option<String>,
    published_at: Option<String>,
    #[serde(default)]
    assets: Vec<ReleaseAsset>,
}

fn valid_repository(value: &str) -> bool {
    let parts: Vec<_> = value.split('/').collect();
    parts.len() == 2
        && parts.iter().all(|part| {
            !part.is_empty()
                && *part != "."
                && *part != ".."
                && part.len() <= 100
                && part
                    .bytes()
                    .all(|c| c.is_ascii_alphanumeric() || b"-_.".contains(&c))
        })
}

pub fn source() -> UpdateSource {
    let repository =
        serde_json::from_str::<ReleaseConfig>(include_str!("../../release-config.json"))
            .ok()
            .and_then(|config| config.github_repository)
            .filter(|value| valid_repository(value));
    #[cfg(feature = "qa-webview")]
    let repository = std::env::var("UNI_SWITCH_QA_UPDATE_REPOSITORY")
        .ok()
        .filter(|value| valid_repository(value))
        .or(repository);
    UpdateSource {
        current_version: env!("CARGO_PKG_VERSION").into(),
        repository,
    }
}

fn version(value: &str) -> Result<([u64; 3], &str)> {
    let value = value.strip_prefix('v').unwrap_or(value);
    let parts: Vec<_> = value.split('.').collect();
    if parts.len() != 3
        || parts.iter().any(|part| {
            part.is_empty()
                || !part.bytes().all(|c| c.is_ascii_digit())
                || (part.len() > 1 && part.starts_with('0'))
        })
    {
        return Err(AppError::new(
            "update_version",
            "发布版本格式不正确，请查看 GitHub 发布页",
        ));
    }
    let mut numbers = [0; 3];
    for (i, part) in parts.iter().enumerate() {
        numbers[i] = part.parse().map_err(|_| {
            AppError::new("update_version", "发布版本格式不正确，请查看 GitHub 发布页")
        })?;
    }
    Ok((numbers, value))
}

fn trusted_url(value: &str, expected: &str) -> bool {
    let Ok(expected) = Url::parse(expected) else {
        return false;
    };
    Url::parse(value).is_ok_and(|url| {
        let actual_parts: Vec<_> = url.path().split('/').collect();
        let expected_parts: Vec<_> = expected.path().split('/').collect();
        url.scheme() == "https"
            && url.host_str() == Some("github.com")
            && url.username().is_empty()
            && url.password().is_none()
            && url.port().is_none()
            && url.query().is_none()
            && url.fragment().is_none()
            && actual_parts.len() == expected_parts.len()
            && actual_parts.len() >= 3
            && actual_parts
                .iter()
                .zip(expected_parts.iter())
                .enumerate()
                .all(|(index, (actual, expected))| {
                    if index == 1 || index == 2 {
                        actual.eq_ignore_ascii_case(expected)
                    } else {
                        actual == expected
                    }
                })
    })
}

fn parse_release(body: &[u8], repository: &str, current: &str) -> Result<UpdateCheck> {
    let release: Release = serde_json::from_slice(body).map_err(|_| {
        AppError::new(
            "update_response",
            "GitHub 返回了无法读取的发布信息，请稍后重试",
        )
    })?;
    if release.draft || release.prerelease {
        return Err(AppError::new(
            "update_unpublished",
            "暂时没有可用的正式版本",
        ));
    }
    let (latest, latest_version) = version(&release.tag_name)?;
    let (installed, _) = version(current)?;
    let release_url = format!(
        "https://github.com/{repository}/releases/tag/{}",
        release.tag_name
    );
    if !trusted_url(&release.html_url, &release_url) {
        return Err(AppError::new(
            "update_response",
            "发布页地址与更新仓库不一致，未打开下载地址",
        ));
    }
    let installer = format!("uni-switch_{latest_version}_x64-setup.exe");
    let download_url = release.assets.iter().find_map(|asset| {
        let expected = format!(
            "https://github.com/{repository}/releases/download/{}/{installer}",
            release.tag_name
        );
        (asset.name == installer && trusted_url(&asset.browser_download_url, &expected))
            .then(|| asset.browser_download_url.clone())
    });
    Ok(UpdateCheck {
        current_version: current.into(),
        latest_version: latest_version.into(),
        available: latest > installed,
        repository: repository.into(),
        release_url,
        download_url,
        notes: release
            .body
            .unwrap_or_default()
            .chars()
            .take(6000)
            .collect(),
        published_at: release.published_at,
        checked_at: SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_secs(),
    })
}

async fn fetch_release(endpoint: &str, repository: &str, current: &str) -> Result<UpdateCheck> {
    let client = reqwest::Client::builder()
        .connect_timeout(Duration::from_secs(5))
        .timeout(Duration::from_secs(15))
        .redirect(reqwest::redirect::Policy::none())
        .user_agent(format!("uni-switch/{current}"))
        .build()
        .map_err(|_| AppError::new("update_network", "更新检测未能启动，请稍后重试"))?;
    let mut response = client
        .get(endpoint)
        .header("Accept", "application/vnd.github+json")
        .header("X-GitHub-Api-Version", "2022-11-28")
        .send()
        .await
        .map_err(|_| AppError::new("update_network", "无法连接 GitHub，请检查网络后重试"))?;
    match response.status().as_u16() {
        404 => {
            return Err(AppError::new(
                "update_unpublished",
                "仓库尚未发布正式版本，或发布仓库暂时不可访问",
            ))
        }
        403 | 429 => {
            return Err(AppError::new(
                "update_rate_limit",
                "GitHub 暂时限制了更新查询，请稍后重试",
            ))
        }
        _ if !response.status().is_success() => {
            return Err(AppError::new(
                "update_network",
                "GitHub 暂时不可用，请稍后重试",
            ))
        }
        _ => {}
    }
    if response
        .content_length()
        .is_some_and(|length| length > MAX_RELEASE_BYTES as u64)
    {
        return Err(AppError::new(
            "update_response",
            "发布信息过大，请直接查看 GitHub 发布页",
        ));
    }
    let mut body = Vec::new();
    while let Some(chunk) = response
        .chunk()
        .await
        .map_err(|_| AppError::new("update_network", "发布信息读取中断，请重试"))?
    {
        if body.len() + chunk.len() > MAX_RELEASE_BYTES {
            return Err(AppError::new(
                "update_response",
                "发布信息过大，请直接查看 GitHub 发布页",
            ));
        }
        body.extend_from_slice(&chunk);
    }
    parse_release(&body, repository, current)
}

pub async fn check() -> Result<UpdateCheck> {
    let source = source();
    let repository = source
        .repository
        .ok_or_else(|| AppError::new("update_not_configured", "此构建尚未绑定 GitHub 发布仓库"))?;
    let endpoint = format!("https://api.github.com/repos/{repository}/releases/latest");
    #[cfg(feature = "qa-webview")]
    let endpoint = std::env::var("UNI_SWITCH_QA_UPDATE_ENDPOINT")
        .ok()
        .filter(|value| {
            Url::parse(value)
                .is_ok_and(|url| url.scheme() == "http" && url.host_str() == Some("127.0.0.1"))
        })
        .unwrap_or(endpoint);
    fetch_release(&endpoint, &repository, &source.current_version).await
}

pub fn open_release(value: &str) -> Result<()> {
    let repository = source()
        .repository
        .ok_or_else(|| AppError::new("update_not_configured", "此构建尚未绑定 GitHub 发布仓库"))?;
    let prefix = format!("https://github.com/{repository}/releases");
    let allowed = value == prefix
        || value
            .strip_prefix(&format!("{prefix}/tag/"))
            .is_some_and(|tag| version(tag).is_ok());
    // Opening the release page lets GitHub present checksums and both packages.
    if !allowed || !trusted_url(value, value) {
        return Err(AppError::new(
            "update_url",
            "只能打开此应用的 GitHub 发布页",
        ));
    }
    #[cfg(feature = "qa-webview")]
    if let Some(marker) = std::env::var_os("UNI_SWITCH_QA_UPDATE_OPEN_MARKER") {
        std::fs::write(marker, value)
            .map_err(|_| AppError::new("update_open", "隔离测试的发布页记录未完成"))?;
        return Ok(());
    }
    #[cfg(windows)]
    {
        use windows_sys::Win32::UI::{Shell::ShellExecuteW, WindowsAndMessaging::SW_SHOWNORMAL};
        let operation: Vec<u16> = "open\0".encode_utf16().collect();
        let target: Vec<u16> = value.encode_utf16().chain(Some(0)).collect();
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
                "update_open",
                "未能打开浏览器，请复制发布页地址后手动打开",
            ));
        }
        Ok(())
    }
    #[cfg(not(windows))]
    Err(AppError::new(
        "update_open",
        "请复制发布页地址后在浏览器打开",
    ))
}

#[cfg(test)]
mod tests;
