use super::*;
use crate::updates::parse_release_for_platform;
use axum::{http::HeaderMap, routing::get, Router};
use serde_json::json;

fn bytes() -> Vec<u8> {
    let mut bytes = vec![42; 4096];
    bytes[..2].copy_from_slice(b"MZ");
    bytes
}
fn hash(bytes: &[u8]) -> String {
    format!("{:x}", Sha256::digest(bytes))
}
fn release(bytes: &[u8], digest: bool) -> UpdateCheck {
    let mut assets = vec![
        json!({"name":"uni-switch_99.0.0_x64-setup.exe", "size":bytes.len(),
        "digest": if digest { Some(format!("sha256:{}", hash(bytes))) } else { None },
        "browser_download_url":"https://github.com/example/uni-switch/releases/download/v99.0.0/uni-switch_99.0.0_x64-setup.exe"}),
    ];
    if !digest {
        assets.push(json!({"name":"SHA256SUMS.txt", "browser_download_url":"https://github.com/example/uni-switch/releases/download/v99.0.0/SHA256SUMS.txt"}));
    }
    parse_release_for_platform(&serde_json::to_vec(&json!({"tag_name":"v99.0.0", "html_url":"https://github.com/example/uni-switch/releases/tag/v99.0.0", "assets":assets})).unwrap(), "example/uni-switch", "0.5.20", "windows", "x86_64").unwrap()
}
async fn fixture(bytes: Vec<u8>, checksum: String) -> (String, tokio::task::JoinHandle<()>) {
    let app = Router::new()
        .route(
            "/example/uni-switch/releases/download/v99.0.0/uni-switch_99.0.0_x64-setup.exe",
            get(move |headers: HeaderMap| {
                let bytes = bytes.clone();
                async move {
                    assert!(headers.get("authorization").is_none());
                    assert!(headers.get("x-api-key").is_none());
                    bytes
                }
            }),
        )
        .route(
            "/example/uni-switch/releases/download/v99.0.0/SHA256SUMS.txt",
            get(move || {
                let checksum = checksum.clone();
                async move { checksum }
            }),
        );
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let origin = format!("http://{}", listener.local_addr().unwrap());
    let task = tokio::spawn(async move { axum::serve(listener, app).await.unwrap() });
    (origin, task)
}
#[tokio::test]
async fn verifies_both_github_digest_and_release_checksum_without_supplier_auth() {
    for digest in [true, false] {
        let bytes = bytes();
        let release = release(&bytes, digest);
        let (origin, task) = fixture(
            bytes.clone(),
            format!("{}  uni-switch_99.0.0_x64-setup.exe\n", hash(&bytes)),
        )
        .await;
        let manager = UpdateManager::default();
        let (status, _) = manager.begin("99.0.0").unwrap();
        let package = manager
            .download(&status.id, "99.0.0", &release, Some(&origin))
            .await
            .unwrap();
        assert_eq!(package.hash, hash(&bytes));
        let path = package.directory.path().join(&package.name);
        manager.finish(&status.id, Ok(package));
        assert_eq!(manager.status().unwrap().unwrap().phase, "ready");
        assert_eq!(std::fs::read(&path).unwrap(), bytes);
        // No installer can launch without the exact opaque receipt.
        assert!(manager
            .install_using("wrong-receipt", |_| panic!("must not execute"))
            .is_err());
        manager.cancel(&status.id).unwrap();
        assert!(!path.exists());
        task.abort();
    }
}
#[tokio::test]
async fn rejects_corrupt_truncated_or_wrong_format_downloads_and_changed_versions() {
    let original = bytes();
    for mut data in [vec![42; 4096], original[..1000].to_vec(), original.clone()] {
        let mut release = release(&original, true);
        if data == original {
            data[..2].copy_from_slice(b"XX");
            release.asset.as_mut().unwrap().digest = Some(format!("sha256:{}", hash(&data)));
        }
        let (origin, task) = fixture(data, String::new()).await;
        let manager = UpdateManager::default();
        let (status, _) = manager.begin("99.0.0").unwrap();
        assert_eq!(
            manager
                .download(&status.id, "99.0.0", &release, Some(&origin))
                .await
                .err()
                .unwrap()
                .code,
            "update_checksum"
        );
        task.abort();
    }
    let manager = UpdateManager::default();
    let (status, _) = manager.begin("99.0.1").unwrap();
    assert_eq!(
        manager
            .download(&status.id, "99.0.1", &release(&original, true), None)
            .await
            .err()
            .unwrap()
            .code,
        "update_changed"
    );
}
#[tokio::test]
async fn refuses_tampered_cache_before_install_and_prevents_repeat_installation() {
    let bytes = bytes();
    let (origin, task) = fixture(bytes.clone(), String::new()).await;
    let manager = UpdateManager::default();
    let (status, _) = manager.begin("99.0.0").unwrap();
    let package = manager
        .download(&status.id, "99.0.0", &release(&bytes, true), Some(&origin))
        .await
        .unwrap();
    let path = package.directory.path().join(&package.name);
    manager.finish(&status.id, Ok(package));
    std::fs::write(&path, vec![0; bytes.len()]).unwrap();
    assert_eq!(
        manager
            .install_using(&status.id, |_| panic!("must not install tampered bytes"))
            .unwrap_err()
            .code,
        "update_checksum"
    );
    assert!(!path.exists());
    let (status, _) = manager.begin("99.0.0").unwrap();
    let package = manager
        .download(&status.id, "99.0.0", &release(&bytes, true), Some(&origin))
        .await
        .unwrap();
    let directory = package.directory.path().to_owned();
    manager.finish(&status.id, Ok(package));
    let result = manager
        .install_using(&status.id, |path| {
            assert_eq!(std::fs::read(path).unwrap(), bytes);
            Ok(InstallResult {
                exit_required: false,
                message: "fake install".into(),
            })
        })
        .unwrap();
    assert!(!result.exit_required);
    assert!(manager
        .install_using(&status.id, |_| panic!("no double install"))
        .is_err());
    assert!(directory
        .file_name()
        .unwrap()
        .to_string_lossy()
        .starts_with("uni-switch-update-"));
    std::fs::remove_dir_all(directory).unwrap(); // Only this test's explicit TempDir.
    task.abort();
}
#[tokio::test]
async fn cancellation_interrupts_download_and_new_attempt_cannot_receive_old_result() {
    let bytes = bytes();
    let app = Router::new().route(
        "/example/uni-switch/releases/download/v99.0.0/uni-switch_99.0.0_x64-setup.exe",
        get(|| async {
            tokio::time::sleep(Duration::from_secs(10)).await;
            Vec::<u8>::new()
        }),
    );
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let origin = format!("http://{}", listener.local_addr().unwrap());
    let server = tokio::spawn(async move { axum::serve(listener, app).await.unwrap() });
    let manager = UpdateManager::default();
    let (status, cancel) = manager.begin("99.0.0").unwrap();
    assert!(manager.begin("99.0.0").is_err());
    let worker = manager.clone();
    let id = status.id.clone();
    let task = tokio::spawn(async move {
        let fixture_release = release(&bytes, true);
        tokio::select! {
            biased;
            _ = cancel.notified() => {},
            result = worker.download(&id, "99.0.0", &fixture_release, Some(&origin)) => worker.finish(&id,result),
        }
    });
    tokio::time::sleep(Duration::from_millis(20)).await;
    manager.cancel(&status.id).unwrap();
    tokio::time::timeout(Duration::from_secs(1), task)
        .await
        .unwrap()
        .unwrap();
    assert_eq!(manager.status().unwrap().unwrap().phase, "cancelled");
    let (next, _) = manager.begin("99.0.0").unwrap();
    manager.finish(&status.id, Err(checksum_error()));
    assert_eq!(manager.status().unwrap().unwrap().id, next.id);
    server.abort();
}
#[test]
fn checksum_selection_is_exact_and_redirect_hosts_are_restricted() {
    let value = "a".repeat(64);
    assert_eq!(
        checksum_from_list(
            format!("{value} *installer.exe\n").as_bytes(),
            "installer.exe"
        )
        .unwrap(),
        value
    );
    for content in [
        format!("{value} other.exe"),
        "bad installer.exe".to_string(),
        format!("{value} installer.exe\n{value} installer.exe"),
    ] {
        assert!(checksum_from_list(content.as_bytes(), "installer.exe").is_err());
    }
    for url in [
        "http://github.com/a",
        "https://evil.test/a",
        "https://github.com.evil.test/a",
        "https://user@github.com/a",
        "https://objects.githubusercontent.com:123/a",
    ] {
        assert!(!download_host(&Url::parse(url).unwrap()));
    }
    assert!(download_host(
        &Url::parse("https://release-assets.githubusercontent.com/a?signature=allowed").unwrap()
    ));
    let mut release = release(&bytes(), true);
    release.remote_update_available = false;
    assert_eq!(release.latest_version, "99.0.0");
}

#[test]
fn validates_native_package_formats_without_launching_them() {
    let directory = tempfile::tempdir().unwrap();
    for (name, prefix, trailer) in [
        ("package.exe", &b"MZ"[..], None),
        ("package.deb", &b"!<arch>\n"[..], None),
        ("package.AppImage", &b"\x7fELF"[..], None),
        ("package.dmg", &b"header"[..], Some(&b"koly"[..])),
    ] {
        let path = directory.path().join(name);
        let mut bytes = vec![0; 4096];
        bytes[..prefix.len()].copy_from_slice(prefix);
        if let Some(trailer) = trailer {
            bytes[4096 - 512..4096 - 508].copy_from_slice(trailer);
        }
        std::fs::write(&path, &bytes).unwrap();
        assert!(validate_package(&path).is_ok());
        bytes.fill(0);
        std::fs::write(&path, bytes).unwrap();
        assert!(validate_package(&path).is_err());
    }
}
