//! Downloads only the configured release's native installer. No supplier credentials,
//! caller-provided URLs or paths ever reach the network or process launcher.
use super::{check, UpdateCheck};
use crate::error::{AppError, Result};
use serde::Serialize;
use sha2::{Digest, Sha256};
use std::{
    io::{Read, Write},
    path::Path,
    sync::{Arc, Mutex},
    time::Duration,
};
use tokio::sync::Notify;
use url::Url;

pub(super) const MAX_INSTALLER_BYTES: u64 = 1024 * 1024 * 1024;
const MAX_CHECKSUM_BYTES: usize = 256 * 1024;

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct DownloadStatus {
    pub id: String,
    pub version: String,
    pub phase: String,
    pub downloaded: u64,
    pub total: u64,
    pub message: String,
}
#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct InstallResult {
    pub exit_required: bool,
    pub message: String,
}
struct Job {
    status: DownloadStatus,
    cancel: Arc<Notify>,
    package: Option<Package>,
}
struct Package {
    directory: tempfile::TempDir,
    name: String,
    hash: String,
    size: u64,
}
#[derive(Clone, Default)]
pub struct UpdateManager(Arc<Mutex<Option<Job>>>);

fn invalid(message: &str) -> AppError {
    AppError::new("update_download", message)
}
fn checksum_error() -> AppError {
    AppError::new(
        "update_checksum",
        "安装包校验失败，未启动安装。请重新下载或前往 GitHub 手动下载。",
    )
}
fn is_terminal(phase: &str) -> bool {
    matches!(phase, "cancelled" | "failed" | "completed")
}

impl UpdateManager {
    fn locked(&self) -> Result<std::sync::MutexGuard<'_, Option<Job>>> {
        self.0
            .lock()
            .map_err(|_| invalid("更新服务忙，请重新打开软件"))
    }
    pub fn status(&self) -> Result<Option<DownloadStatus>> {
        Ok(self.locked()?.as_ref().map(|job| job.status.clone()))
    }
    fn begin(&self, version: &str) -> Result<(DownloadStatus, Arc<Notify>)> {
        super::version(version)?;
        let mut guard = self.locked()?;
        if guard
            .as_ref()
            .is_some_and(|job| !is_terminal(&job.status.phase))
        {
            return Err(invalid("已有更新正在进行，请先完成或取消"));
        }
        let status = DownloadStatus {
            id: uuid::Uuid::new_v4().to_string(),
            version: version.into(),
            phase: "checking".into(),
            downloaded: 0,
            total: 0,
            message: "正在确认版本和安装包…".into(),
        };
        let cancel = Arc::new(Notify::new());
        *guard = Some(Job {
            status: status.clone(),
            cancel: cancel.clone(),
            package: None,
        });
        Ok((status, cancel))
    }
    pub fn start(&self, version: &str) -> Result<DownloadStatus> {
        let (status, cancel) = self.begin(version)?;
        let manager = self.clone();
        let id = status.id.clone();
        let version = status.version.clone();
        tokio::spawn(async move {
            let result = tokio::select! {
                biased;
                _ = cancel.notified() => return,
                result = async {
                    let release = check().await?;
                    manager.download(&id, &version, &release, None).await
                } => result,
            };
            manager.finish(&id, result);
        });
        Ok(status)
    }
    fn change(&self, id: &str, change: impl FnOnce(&mut Job)) -> Result<()> {
        let mut guard = self.locked()?;
        let job = guard
            .as_mut()
            .filter(|job| job.status.id == id && !is_terminal(&job.status.phase))
            .ok_or_else(|| invalid("本次更新已取消或已结束，请重新选择更新方式"))?;
        change(job);
        Ok(())
    }
    fn finish(&self, id: &str, result: Result<Package>) {
        let _ = self.change(id, |job| match result {
            Ok(package) => {
                job.status.phase = "ready".into();
                job.status.message = "下载完成，SHA256 校验通过。点击安装更新继续。".into();
                job.package = Some(package);
            }
            Err(error) => {
                job.status.phase = "failed".into();
                job.status.message = error.message;
            }
        });
    }
    pub fn cancel(&self, id: &str) -> Result<()> {
        self.change(id, |job| {
            if job.status.phase != "installing" {
                job.status.phase = "cancelled".into();
                job.status.message = "已取消更新，客户端配置未改动。".into();
                job.package = None;
                job.cancel.notify_one();
            }
        })
    }
    async fn download(
        &self,
        id: &str,
        expected: &str,
        release: &UpdateCheck,
        test_origin: Option<&str>,
    ) -> Result<Package> {
        if release.latest_version != expected || !release.available {
            return Err(AppError::new(
                "update_changed",
                "GitHub 最新版本已变化，请重新检测并选择更新方式。",
            ));
        }
        if !release.remote_update_available {
            return Err(invalid(
                "此版本未提供可校验的本系统安装包，请选择 GitHub 手动下载。",
            ));
        }
        let asset = release
            .asset
            .as_ref()
            .ok_or_else(|| invalid("未找到本系统安装包"))?;
        let client = download_client()?;
        let mut expected_hash = asset_digest(asset.digest.as_deref());
        if expected_hash.is_none() {
            let url = release.checksum_url.as_deref().ok_or_else(checksum_error)?;
            let mut response = request(&client, url, test_origin).await?;
            let mut bytes = Vec::new();
            while let Some(chunk) = response
                .chunk()
                .await
                .map_err(|_| invalid("校验清单读取中断，请重试"))?
            {
                if bytes.len() + chunk.len() > MAX_CHECKSUM_BYTES {
                    return Err(checksum_error());
                }
                bytes.extend_from_slice(&chunk);
            }
            expected_hash = Some(checksum_from_list(&bytes, &asset.name)?);
        }
        let expected_hash = expected_hash.ok_or_else(checksum_error)?;
        self.change(id, |job| {
            job.status.phase = "downloading".into();
            job.status.total = asset.size;
            job.status.message = "正在从 GitHub 下载安装包…".into();
        })?;
        let mut response = request(&client, &asset.browser_download_url, test_origin).await?;
        if response
            .content_length()
            .is_some_and(|size| size != asset.size)
        {
            return Err(checksum_error());
        }
        let directory = tempfile::Builder::new()
            .prefix("uni-switch-update-")
            .tempdir()
            .map_err(|_| invalid("无法创建更新缓存，请检查磁盘空间和权限"))?;
        let file_path = directory.path().join(&asset.name);
        let mut file = std::fs::OpenOptions::new()
            .write(true)
            .create_new(true)
            .open(&file_path)
            .map_err(|_| invalid("无法保存安装包，请检查磁盘空间和权限"))?;
        let mut hash = Sha256::new();
        let mut downloaded = 0;
        while let Some(chunk) = tokio::time::timeout(Duration::from_secs(30), response.chunk())
            .await
            .map_err(|_| invalid("下载超时，请重试或前往 GitHub 手动下载"))?
            .map_err(|_| invalid("安装包下载中断，请重试"))?
        {
            downloaded += chunk.len() as u64;
            if downloaded > asset.size || downloaded > MAX_INSTALLER_BYTES {
                return Err(checksum_error());
            }
            file.write_all(&chunk)
                .map_err(|_| invalid("保存安装包失败，请检查磁盘空间"))?;
            hash.update(&chunk);
            self.change(id, |job| {
                job.status.downloaded = downloaded;
            })?;
        }
        self.change(id, |job| {
            job.status.phase = "verifying".into();
            job.status.message = "正在校验安装包…".into();
        })?;
        file.sync_all()
            .map_err(|_| invalid("保存安装包失败，请检查磁盘空间"))?;
        drop(file);
        if downloaded != asset.size || format!("{:x}", hash.finalize()) != expected_hash {
            return Err(checksum_error());
        }
        validate_package(&file_path)?;
        Ok(Package {
            directory,
            name: asset.name.clone(),
            hash: expected_hash,
            size: asset.size,
        })
    }
    pub fn install(&self, id: &str) -> Result<InstallResult> {
        self.install_using(id, launch_installer)
    }
    fn install_using(
        &self,
        id: &str,
        launch: impl FnOnce(&Path) -> Result<InstallResult>,
    ) -> Result<InstallResult> {
        let mut guard = self.locked()?;
        let job = guard
            .as_mut()
            .filter(|job| job.status.id == id && job.status.phase == "ready")
            .ok_or_else(|| invalid("没有已校验的安装包，请先下载"))?;
        let package = job.package.as_ref().ok_or_else(checksum_error)?;
        let path = package.directory.path().join(&package.name);
        // Recheck the exact cached bytes immediately before handing them to the OS.
        if verify_file(&path, package.size, &package.hash).is_err()
            || validate_package(&path).is_err()
        {
            job.status.phase = "failed".into();
            job.status.message = checksum_error().message;
            job.package = None;
            return Err(checksum_error());
        }
        let result = launch(&path)?;
        job.status.phase = "completed".into();
        job.status.message = result.message.clone();
        // The installer reads this path after the app exits. Keep only this verified
        // directory; other attempts are TempDirs and are automatically cleaned up.
        if let Some(package) = job.package.take() {
            let _ = package.directory.keep();
        }
        Ok(result)
    }
}

pub(super) fn asset_digest(value: Option<&str>) -> Option<String> {
    value
        .and_then(|v| v.strip_prefix("sha256:"))
        .filter(|v| v.len() == 64 && v.bytes().all(|b| b.is_ascii_hexdigit()))
        .map(str::to_ascii_lowercase)
}
fn checksum_from_list(bytes: &[u8], name: &str) -> Result<String> {
    let text = std::str::from_utf8(bytes).map_err(|_| checksum_error())?;
    let mut found = None;
    for line in text.lines() {
        let parts: Vec<_> = line.split_whitespace().collect();
        if parts.len() == 2 && parts[1].trim_start_matches('*') == name {
            let hash =
                asset_digest(Some(&format!("sha256:{}", parts[0]))).ok_or_else(checksum_error)?;
            if found.replace(hash).is_some() {
                return Err(checksum_error());
            }
        }
    }
    found.ok_or_else(checksum_error)
}
fn download_host(url: &Url) -> bool {
    url.scheme() == "https"
        && url.username().is_empty()
        && url.password().is_none()
        && url.port().is_none()
        && matches!(
            url.host_str(),
            Some(
                "github.com"
                    | "release-assets.githubusercontent.com"
                    | "objects.githubusercontent.com"
            )
        )
}
fn download_client() -> Result<reqwest::Client> {
    reqwest::Client::builder()
        .connect_timeout(Duration::from_secs(10))
        .timeout(Duration::from_secs(600))
        .user_agent(format!("uni-switch/{}", env!("CARGO_PKG_VERSION")))
        .redirect(reqwest::redirect::Policy::custom(|attempt| {
            if attempt.previous().len() >= 8 || !download_host(attempt.url()) {
                attempt.error("untrusted update redirect")
            } else {
                attempt.follow()
            }
        }))
        .build()
        .map_err(|_| invalid("更新下载未能启动"))
}
async fn request(
    client: &reqwest::Client,
    url: &str,
    test_origin: Option<&str>,
) -> Result<reqwest::Response> {
    let parsed = Url::parse(url).map_err(|_| invalid("安装包地址无效"))?;
    if !download_host(&parsed) {
        return Err(invalid("安装包地址不可信"));
    }
    // Loopback asset fixtures exist only in QA binaries and unit tests.
    let request_url = url.to_owned();
    #[cfg(any(test, feature = "qa-webview"))]
    let request_url = {
        let origin = test_origin
            .map(str::to_owned)
            .or_else(|| std::env::var("UNI_SWITCH_QA_UPDATE_ASSET_ORIGIN").ok());
        match origin {
            Some(origin)
                if Url::parse(&origin).is_ok_and(|u| {
                    u.scheme() == "http"
                        && u.host_str() == Some("127.0.0.1")
                        && u.username().is_empty()
                        && u.password().is_none()
                }) =>
            {
                format!("{}{}", origin.trim_end_matches('/'), parsed.path())
            }
            _ => request_url,
        }
    };
    #[cfg(not(any(test, feature = "qa-webview")))]
    let _ = test_origin;
    let response = client
        .get(request_url)
        .send()
        .await
        .map_err(|_| invalid("无法下载 GitHub 安装包，请检查网络或使用手动下载"))?;
    if !response.status().is_success() {
        return Err(invalid("GitHub 安装包暂时不可下载，请重试或使用手动下载"));
    }
    Ok(response)
}
fn verify_file(path: &Path, size: u64, expected: &str) -> Result<()> {
    let mut file = std::fs::File::open(path).map_err(|_| checksum_error())?;
    if file.metadata().map_err(|_| checksum_error())?.len() != size {
        return Err(checksum_error());
    }
    let mut hash = Sha256::new();
    let mut buffer = [0; 64 * 1024];
    loop {
        let length = file.read(&mut buffer).map_err(|_| checksum_error())?;
        if length == 0 {
            break;
        }
        hash.update(&buffer[..length]);
    }
    if format!("{:x}", hash.finalize()) != expected {
        return Err(checksum_error());
    }
    Ok(())
}
fn validate_package(path: &Path) -> Result<()> {
    let mut file = std::fs::File::open(path).map_err(|_| checksum_error())?;
    let mut prefix = [0; 8];
    file.read_exact(&mut prefix).map_err(|_| checksum_error())?;
    let extension = path.extension().and_then(|s| s.to_str()).unwrap_or("");
    let valid = match extension {
        "exe" => prefix.starts_with(b"MZ"),
        "deb" => &prefix == b"!<arch>\n",
        "AppImage" => prefix.starts_with(b"\x7fELF"),
        "dmg" => {
            use std::io::{Seek, SeekFrom};
            file.seek(SeekFrom::End(-512))
                .map_err(|_| checksum_error())?;
            file.read_exact(&mut prefix[..4])
                .map_err(|_| checksum_error())?;
            &prefix[..4] == b"koly"
        }
        _ => false,
    };
    if valid {
        Ok(())
    } else {
        Err(checksum_error())
    }
}
fn launch_installer(path: &Path) -> Result<InstallResult> {
    #[cfg(feature = "qa-webview")]
    {
        // A QA build must NEVER launch a real installer or exit a real app.
        let marker = std::env::var_os("UNI_SWITCH_QA_UPDATE_INSTALL_MARKER")
            .ok_or_else(|| invalid("隔离测试缺少安装记录位置，已阻止启动安装"))?;
        std::fs::write(marker, path.to_string_lossy().as_bytes())
            .map_err(|_| invalid("未能记录隔离安装测试"))?;
        return Ok(InstallResult {
            exit_required: false,
            message: "隔离测试：安装包已校验，未执行真实安装。".into(),
        });
    }
    #[cfg(not(feature = "qa-webview"))]
    {
        #[cfg(target_os = "windows")]
        {
            std::process::Command::new(path)
                .spawn()
                .map_err(|_| invalid("未能启动安装向导，请重试或选择 GitHub 手动下载"))?;
            Ok(InstallResult {
                exit_required: true,
                message: "安装向导已启动，uni-switch 即将退出。完成安装后重新打开软件。".into(),
            })
        }
        #[cfg(target_os = "macos")]
        {
            std::process::Command::new("open")
                .arg(path)
                .spawn()
                .map_err(|_| invalid("未能打开安装包，请选择 GitHub 手动下载"))?;
            Ok(InstallResult { exit_required: false, message: "DMG 安装包已打开。请从托盘退出 uni-switch，再将新版拖入 Applications 完成替换。".into() })
        }
        #[cfg(target_os = "linux")]
        {
            let appimage = path.extension().is_some_and(|ext| ext == "AppImage");
            let target = if appimage {
                path.parent().ok_or_else(checksum_error)?
            } else {
                path
            };
            std::process::Command::new("xdg-open")
                .arg(target)
                .spawn()
                .map_err(|_| invalid("未能打开系统安装程序，请选择 GitHub 手动下载"))?;
            Ok(InstallResult { exit_required: false, message: if appimage { "下载目录已打开。退出旧版后，用已下载的 AppImage 替换旧文件、授予执行权限并重新启动。" } else { "已请求打开系统安装程序。退出旧版并完成 DEB 安装后，重新启动 uni-switch。" }.into() })
        }
        #[cfg(not(any(target_os = "windows", target_os = "macos", target_os = "linux")))]
        {
            let _ = path;
            Err(invalid("当前系统请使用 GitHub 手动下载"))
        }
    }
}

#[cfg(test)]
mod tests;
